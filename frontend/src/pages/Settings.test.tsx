import { create } from "@bufbuild/protobuf";
import { fireEvent, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { App } from "../App";
import { DownloadClientConfigSchema } from "../gen/omnibus/v1/download_client_pb";
import { makeDownloadClientTransport, renderWithProviders } from "../test/render";

const sampleConfig = create(DownloadClientConfigSchema, {
  url: "http://sab.test",
  category: "comics",
  configured: true,
});

describe("settings page", () => {
  it("renders the download client section with the current config", async () => {
    const transport = makeDownloadClientTransport({
      getDownloadClientConfig: () => ({
        config: sampleConfig,
      }),
    });
    renderWithProviders(<App initialRoute="settings" />, transport);

    expect(await screen.findByText("Download Client")).toBeInTheDocument();
    expect(await screen.findByText("http://sab.test")).toBeInTheDocument();
    expect(screen.getByText("Configured")).toBeInTheDocument();
  });

  it("renders the API key input as a password field when editing", async () => {
    const transport = makeDownloadClientTransport({
      getDownloadClientConfig: () => ({
        config: sampleConfig,
      }),
    });
    renderWithProviders(<App initialRoute="settings" />, transport);

    expect(await screen.findByText("http://sab.test")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /edit/i }));

    const apiKey = screen.getByLabelText(/api key/i);
    expect(apiKey).toHaveAttribute("type", "password");
  });

  it("shows a connected indicator when the Test connection probe succeeds", async () => {
    const transport = makeDownloadClientTransport({
      getDownloadClientConfig: () => ({ config: sampleConfig }),
      testDownloadClientConfig: () => ({ ok: true, detail: "connected" }),
    });
    renderWithProviders(<App initialRoute="settings" />, transport);

    expect(await screen.findByText("http://sab.test")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /test connection/i }));

    expect(await screen.findByText(/connected/i)).toBeInTheDocument();
  });

  it("shows the failure detail when the Test connection probe fails", async () => {
    const transport = makeDownloadClientTransport({
      getDownloadClientConfig: () => ({ config: sampleConfig }),
      testDownloadClientConfig: () => ({ ok: false, detail: "not configured" }),
    });
    renderWithProviders(<App initialRoute="settings" />, transport);

    expect(await screen.findByText("http://sab.test")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /test connection/i }));

    expect(await screen.findByText(/not configured/i)).toBeInTheDocument();
  });
});
