import { useState } from "react";

import { Button } from "./ui/button";

// DDLSettingsFormValues mirrors the DDLConfig proto message one-to-one (05-07): a single
// enable toggle, no secrets.
export interface DDLSettingsFormValues {
  enabled: boolean;
}

const emptyValues: DDLSettingsFormValues = {
  enabled: false,
};

// DDLSettingsForm is the edit form for the DDL config (05-07). It exposes a single "Enable
// DDLs" toggle — there is NO numeric DDL-priority control (the order is structural: Newznab
// indexers first, GetComics last). The control stays a native checkbox so it keeps the
// checkbox role the tests assert on.
export function DDLSettingsForm({
  initial,
  pending = false,
  error,
  onSubmit,
  onCancel,
}: {
  initial?: Partial<DDLSettingsFormValues>;
  pending?: boolean;
  error?: string;
  onSubmit: (values: DDLSettingsFormValues) => void;
  onCancel: () => void;
}) {
  const [values, setValues] = useState<DDLSettingsFormValues>({
    ...emptyValues,
    ...initial,
  });

  return (
    <form
      className="flex flex-col gap-4 rounded-xl border border-border bg-card/70 p-5"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit(values);
      }}
    >
      <label className="flex items-center gap-2.5 text-sm font-medium text-foreground">
        <input
          type="checkbox"
          className="size-4 shrink-0 rounded border-input accent-[#7aa2f7]"
          checked={values.enabled}
          onChange={(e) => setValues({ enabled: e.target.checked })}
        />
        Enable DDLs
      </label>

      <p className="text-sm text-muted-foreground">
        When enabled, GetComics is used as a fallback to find releases only when no Newznab
        indexer has an acceptable release for an issue.
      </p>

      {error ? <p className="text-sm text-tn-red">{error}</p> : null}

      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending}>
          Save DDL settings
        </Button>
      </div>
    </form>
  );
}
