import { fireEvent, render, screen } from "@testing-library/react";
import { axe } from "jest-axe";
import { describe, expect, it, vi } from "vitest";

import { AuthMode } from "../gen/omnibus/v1/auth_pb";
import { AuthSettingsForm, type AuthSettingsFormValues } from "./AuthSettingsForm";

describe("AuthSettingsForm", () => {
  it("never seeds the password and keeps the credential on a blank submit", () => {
    const onSubmit = vi.fn<(v: AuthSettingsFormValues) => void>();
    render(
      <AuthSettingsForm
        initial={{ mode: AuthMode.ON, username: "kyle" }}
        onSubmit={onSubmit}
        onCancel={() => {}}
      />,
    );

    // The password field starts empty even though a credential is stored server-side (D-02).
    const password = screen.getByLabelText(/password/i) as HTMLInputElement;
    expect(password.value).toBe("");

    // Submitting without typing a password sends an empty password -> server keeps the hash.
    fireEvent.click(screen.getByRole("button", { name: /save authentication settings/i }));
    expect(onSubmit).toHaveBeenCalledTimes(1);
    expect(onSubmit.mock.calls[0][0].password).toBe("");
    expect(onSubmit.mock.calls[0][0].username).toBe("kyle");
    expect(onSubmit.mock.calls[0][0].mode).toBe(AuthMode.ON);
  });

  it("masks the password field", () => {
    render(<AuthSettingsForm onSubmit={() => {}} onCancel={() => {}} />);
    expect(screen.getByLabelText(/password/i)).toHaveAttribute("type", "password");
  });

  it("offers exactly the three locked mode options", () => {
    render(<AuthSettingsForm onSubmit={() => {}} onCancel={() => {}} />);
    const select = screen.getByLabelText(/authentication mode/i) as HTMLSelectElement;
    const labels = Array.from(select.options).map((o) => o.textContent);
    expect(labels).toEqual(["Off", "On", "On, except local addresses"]);
  });

  it("submits a typed password and a changed mode", () => {
    const onSubmit = vi.fn<(v: AuthSettingsFormValues) => void>();
    render(
      <AuthSettingsForm
        initial={{ mode: AuthMode.OFF, username: "kyle" }}
        onSubmit={onSubmit}
        onCancel={() => {}}
      />,
    );

    fireEvent.change(screen.getByLabelText(/authentication mode/i), {
      target: { value: String(AuthMode.BYPASS_LOCAL) },
    });
    fireEvent.change(screen.getByLabelText(/password/i), { target: { value: "s3cret" } });
    fireEvent.click(screen.getByRole("button", { name: /save authentication settings/i }));

    expect(onSubmit.mock.calls[0][0].mode).toBe(AuthMode.BYPASS_LOCAL);
    expect(onSubmit.mock.calls[0][0].password).toBe("s3cret");
  });

  it("never produces a NaN mode from a malformed option value", () => {
    const onSubmit = vi.fn<(v: AuthSettingsFormValues) => void>();
    render(
      <AuthSettingsForm
        initial={{ mode: AuthMode.ON, username: "kyle" }}
        onSubmit={onSubmit}
        onCancel={() => {}}
      />,
    );

    const select = screen.getByLabelText(/authentication mode/i) as HTMLSelectElement;
    // Simulate a malformed value that Number() would turn into NaN. The guard must reject it
    // and keep the previously-selected known mode (WR-04) rather than submitting NaN.
    fireEvent.change(select, { target: { value: "not-a-number" } });
    fireEvent.click(screen.getByRole("button", { name: /save authentication settings/i }));

    const submitted = onSubmit.mock.calls[0][0];
    expect(Number.isNaN(submitted.mode)).toBe(false);
    expect(submitted.mode).toBe(AuthMode.ON);
  });

  it("has labelled controls and no axe violations", async () => {
    const { container } = render(
      <AuthSettingsForm
        initial={{ mode: AuthMode.ON, username: "kyle" }}
        onSubmit={() => {}}
        onCancel={() => {}}
      />,
    );
    expect(screen.getByLabelText(/username/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/password/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/authentication mode/i)).toBeInTheDocument();
    expect(await axe(container)).toHaveNoViolations();
  });
});
