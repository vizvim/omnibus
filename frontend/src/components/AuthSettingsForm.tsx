import { useState } from "react";

import { AuthMode } from "../gen/omnibus/v1/auth_pb";
import { Button } from "./ui/button";
import { Input } from "./ui/input";

// AuthSettingsFormValues is the editable optional-auth payload the form collects. The
// password is write-only: it is never seeded with the stored value and a blank submit keeps
// the existing credential server-side (D-02, mirrors DownloadClientForm's API key).
export interface AuthSettingsFormValues {
  mode: AuthMode;
  username: string;
  password: string;
}

// modeOptions are the three locked auth states (D-01), mirroring Sonarr/Radarr. The labels
// and helper copy are verbatim from the UI-SPEC Copywriting contract.
const modeOptions: { value: AuthMode; label: string; helper: string }[] = [
  {
    value: AuthMode.OFF,
    label: "Off",
    helper:
      "Anyone who can reach this server can use omnibus. Recommended only on a trusted private network.",
  },
  {
    value: AuthMode.ON,
    label: "On",
    helper: "Require sign-in for every request, including the live status stream.",
  },
  {
    value: AuthMode.BYPASS_LOCAL,
    label: "On, except local addresses",
    helper:
      "Require sign-in, but skip it for requests from this machine and your local network (loopback and private IP ranges). Useful behind a reverse proxy or on a home LAN.",
  },
];

const emptyValues: AuthSettingsFormValues = {
  mode: AuthMode.OFF,
  username: "",
  password: "",
};

const fieldLabel = "flex flex-col gap-1.5 text-sm font-medium text-foreground";
const selectClass =
  "h-9 rounded-md border border-input bg-tn-night/60 px-3 text-sm text-foreground transition-colors focus-visible:border-primary/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40";

// AuthSettingsForm is the edit form for the optional-auth config (AUTH-01): the three-state
// mode select + helper copy, the single-user Username, and a masked Password that is never
// seeded. It copies the DownloadClientForm props/structure exactly.
export function AuthSettingsForm({
  initial,
  pending = false,
  error,
  onSubmit,
  onCancel,
}: {
  initial?: Partial<Pick<AuthSettingsFormValues, "mode" | "username">>;
  pending?: boolean;
  error?: string;
  onSubmit: (values: AuthSettingsFormValues) => void;
  onCancel: () => void;
}) {
  const [values, setValues] = useState<AuthSettingsFormValues>({
    ...emptyValues,
    ...initial,
    password: "", // never seed the masked field with a stored credential (D-02)
  });

  function set<K extends keyof AuthSettingsFormValues>(
    key: K,
    value: AuthSettingsFormValues[K],
  ) {
    setValues((v) => ({ ...v, [key]: value }));
  }

  const helper = modeOptions.find((o) => o.value === values.mode)?.helper;

  return (
    <form
      className="flex flex-col gap-4 rounded-xl border border-border bg-card/70 p-5"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit(values);
      }}
    >
      <label className={fieldLabel}>
        Authentication mode
        <select
          aria-label="Authentication mode"
          className={selectClass}
          value={String(values.mode)}
          onChange={(e) => {
            // Guard against a malformed option value producing NaN (then a meaningless
            // mode the server would silently coerce to Off, disabling auth). Only ever set a
            // known AuthMode (WR-04).
            const next = Number(e.target.value);
            if (modeOptions.some((o) => o.value === next)) {
              set("mode", next as AuthMode);
            }
          }}
        >
          {modeOptions.map((opt) => (
            <option key={opt.value} value={String(opt.value)}>
              {opt.label}
            </option>
          ))}
        </select>
        {helper ? <span className="text-sm font-normal text-muted-foreground">{helper}</span> : null}
      </label>

      <label className={fieldLabel}>
        Username
        <Input
          autoComplete="username"
          value={values.username}
          onChange={(e) => set("username", e.target.value)}
        />
      </label>

      <label className={fieldLabel}>
        Password
        <Input
          type="password"
          autoComplete="new-password"
          placeholder="Leave blank to keep current password"
          value={values.password}
          onChange={(e) => set("password", e.target.value)}
        />
      </label>

      {error ? <p className="text-sm text-tn-red">{error}</p> : null}

      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending}>
          Save authentication settings
        </Button>
      </div>
    </form>
  );
}
