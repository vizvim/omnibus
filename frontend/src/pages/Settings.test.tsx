import { create } from "@bufbuild/protobuf";
import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { App } from "../App";
import { DownloadClientConfigSchema } from "../gen/omnibus/v1/download_client_pb";
import { RenameConfigSchema } from "../gen/omnibus/v1/rename_config_pb";
import { makeSettingsTransport, renderWithProviders } from "../test/render";

const sampleConfig = create(DownloadClientConfigSchema, {
  url: "http://sab.test",
  category: "comics",
  configured: true,
});

const sampleRenameConfig = create(RenameConfigSchema, {
  folderFormat: "$Publisher/$Series ($Year)",
  fileFormat: "$Series $VolumeY $Annual #$Issue ($monthname $Year)",
  renameEnabled: true,
  issuePaddingEnabled: true,
  paddingFormat: "00x",
  replaceSpaces: false,
  lowercase: false,
});

const renameStub = {
  getRenameConfig: () => ({ config: sampleRenameConfig }),
};

describe("settings page", () => {
  it("renders the download client section with the current config", async () => {
    const transport = makeSettingsTransport(
      {
        getDownloadClientConfig: () => ({ config: sampleConfig }),
      },
      renameStub,
    );
    renderWithProviders(<App initialRoute="settings" />, transport);

    expect(await screen.findByText("Download Client")).toBeInTheDocument();
    expect(await screen.findByText("http://sab.test")).toBeInTheDocument();
    expect(screen.getByText("Configured")).toBeInTheDocument();
  });

  it("renders the API key input as a password field when editing", async () => {
    const transport = makeSettingsTransport(
      {
        getDownloadClientConfig: () => ({ config: sampleConfig }),
      },
      renameStub,
    );
    renderWithProviders(<App initialRoute="settings" />, transport);

    expect(await screen.findByText("http://sab.test")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /edit download client/i }));

    const apiKey = screen.getByLabelText(/api key/i);
    expect(apiKey).toHaveAttribute("type", "password");
  });

  it("shows a connected indicator when the Test connection probe succeeds", async () => {
    const transport = makeSettingsTransport(
      {
        getDownloadClientConfig: () => ({ config: sampleConfig }),
        testDownloadClientConfig: () => ({ ok: true, detail: "connected" }),
      },
      renameStub,
    );
    renderWithProviders(<App initialRoute="settings" />, transport);

    expect(await screen.findByText("http://sab.test")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /test connection/i }));

    expect(await screen.findByText(/connected/i)).toBeInTheDocument();
  });

  it("shows the failure detail when the Test connection probe fails", async () => {
    const transport = makeSettingsTransport(
      {
        getDownloadClientConfig: () => ({ config: sampleConfig }),
        testDownloadClientConfig: () => ({ ok: false, detail: "not configured" }),
      },
      renameStub,
    );
    renderWithProviders(<App initialRoute="settings" />, transport);

    expect(await screen.findByText("http://sab.test")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /test connection/i }));

    expect(await screen.findByText(/not configured/i)).toBeInTheDocument();
  });

  it("renders the renaming section with the Mylar defaults", async () => {
    const transport = makeSettingsTransport(
      {
        getDownloadClientConfig: () => ({ config: sampleConfig }),
      },
      renameStub,
    );
    renderWithProviders(<App initialRoute="settings" />, transport);

    expect(await screen.findByText("Renaming")).toBeInTheDocument();
    expect(
      await screen.findByText("$Publisher/$Series ($Year)"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("$Series $VolumeY $Annual #$Issue ($monthname $Year)"),
    ).toBeInTheDocument();
  });

  it("saves edited renaming settings via UpdateRenameConfig", async () => {
    let savedFolder: string | undefined;
    // The stored config is stateful so a refetch after save reflects the saved value.
    let stored = sampleRenameConfig;
    const transport = makeSettingsTransport(
      {
        getDownloadClientConfig: () => ({ config: sampleConfig }),
      },
      {
        getRenameConfig: () => ({ config: stored }),
        updateRenameConfig: (req: never) => {
          savedFolder = (req as { folderFormat: string }).folderFormat;
          stored = create(RenameConfigSchema, {
            ...sampleRenameConfig,
            folderFormat: savedFolder,
          });
          return { config: stored };
        },
      },
    );
    renderWithProviders(<App initialRoute="settings" />, transport);

    expect(await screen.findByText("Renaming")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /edit renaming settings/i }));

    const folderInput = screen.getByDisplayValue("$Publisher/$Series ($Year)");
    fireEvent.change(folderInput, { target: { value: "$Series/$Year" } });
    fireEvent.click(screen.getByRole("button", { name: /save renaming settings/i }));

    expect(await screen.findByText("$Series/$Year")).toBeInTheDocument();
    expect(savedFolder).toBe("$Series/$Year");
  });
});
