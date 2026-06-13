import { useState } from "react";

// DDLSettingsFormValues is the editable DDL-config payload the form collects (05-07). It
// mirrors the DDLConfig proto message one-to-one — a single boolean enable toggle, no
// secrets (the GetComics base URL is a built-in constant, not user-editable).
export interface DDLSettingsFormValues {
  enabled: boolean;
}

const emptyValues: DDLSettingsFormValues = {
  enabled: false,
};

// DDLSettingsForm is the functional-minimal edit form for the DDL config (05-07),
// mirroring RenameSettingsForm. It exposes a single "Enable DDLs" toggle — there is NO
// numeric DDL-priority control (the order is structural: Newznab indexers first, GetComics
// last). Polish (dark mode / a11y) is deferred to Phase 6.
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
      className="flex flex-col gap-3 rounded border border-slate-200 bg-white p-4"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit(values);
      }}
    >
      <label className="flex items-center gap-2 text-sm font-semibold">
        <input
          type="checkbox"
          checked={values.enabled}
          onChange={(e) => setValues({ enabled: e.target.checked })}
        />
        Enable DDLs
      </label>

      <p className="text-sm font-normal text-slate-600">
        When enabled, GetComics is used as a fallback to find releases only when no Newznab
        indexer has an acceptable release for an issue.
      </p>

      {error ? <p className="text-sm text-red-600">{error}</p> : null}

      <div className="flex justify-end gap-2">
        <button
          type="button"
          onClick={onCancel}
          className="rounded bg-slate-100 px-4 py-2 text-sm font-semibold"
        >
          Cancel
        </button>
        <button
          type="submit"
          disabled={pending}
          className="rounded bg-blue-600 px-4 py-2 text-sm font-semibold text-white disabled:opacity-50"
        >
          Save DDL settings
        </button>
      </div>
    </form>
  );
}
