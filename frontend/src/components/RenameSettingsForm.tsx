import { useState } from "react";

// RenameSettingsFormValues is the editable renaming-config payload the form collects
// (D-09). It mirrors the RenameConfig proto message one-to-one — every field is a plain
// string/boolean, no secrets.
export interface RenameSettingsFormValues {
  folderFormat: string;
  fileFormat: string;
  renameEnabled: boolean;
  issuePaddingEnabled: boolean;
  paddingFormat: string;
  replaceSpaces: boolean;
  lowercase: boolean;
}

// paddingOptions are the issue-number padding widths the user can pick (D-07).
const paddingOptions: { value: string; label: string }[] = [
  { value: "00x", label: "3-digit (#001)" },
  { value: "0x", label: "2-digit (#01)" },
  { value: "x", label: "None (#1)" },
];

const emptyValues: RenameSettingsFormValues = {
  folderFormat: "",
  fileFormat: "",
  renameEnabled: true,
  issuePaddingEnabled: true,
  paddingFormat: "00x",
  replaceSpaces: false,
  lowercase: false,
};

// RenameSettingsForm is the functional-minimal edit form for the renaming config (D-09),
// mirroring DownloadClientForm. It exposes the Rename master toggle, folder/file format
// templates, Replace Spaces, Lowercase, and the Issue Number Padding toggle + format
// dropdown. Polish (dark mode / a11y) is deferred to Phase 6.
export function RenameSettingsForm({
  initial,
  pending = false,
  error,
  onSubmit,
  onCancel,
}: {
  initial?: Partial<RenameSettingsFormValues>;
  pending?: boolean;
  error?: string;
  onSubmit: (values: RenameSettingsFormValues) => void;
  onCancel: () => void;
}) {
  const [values, setValues] = useState<RenameSettingsFormValues>({
    ...emptyValues,
    ...initial,
  });

  function set<K extends keyof RenameSettingsFormValues>(
    key: K,
    value: RenameSettingsFormValues[K],
  ) {
    setValues((v) => ({ ...v, [key]: value }));
  }

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
          checked={values.renameEnabled}
          onChange={(e) => set("renameEnabled", e.target.checked)}
        />
        Rename files
      </label>

      <label className="flex flex-col gap-1 text-sm font-semibold">
        Folder Format
        <input
          className="rounded border border-slate-300 px-3 py-2 text-sm font-normal"
          value={values.folderFormat}
          onChange={(e) => set("folderFormat", e.target.value)}
          placeholder="$Publisher/$Series ($Year)"
        />
      </label>

      <label className="flex flex-col gap-1 text-sm font-semibold">
        File Format
        <input
          className="rounded border border-slate-300 px-3 py-2 text-sm font-normal"
          value={values.fileFormat}
          onChange={(e) => set("fileFormat", e.target.value)}
          placeholder="$Series $VolumeY $Annual #$Issue ($monthname $Year)"
        />
      </label>

      <label className="flex items-center gap-2 text-sm font-semibold">
        <input
          type="checkbox"
          checked={values.issuePaddingEnabled}
          onChange={(e) => set("issuePaddingEnabled", e.target.checked)}
        />
        Issue Number Padding
      </label>

      <label className="flex flex-col gap-1 text-sm font-semibold">
        Padding Format
        <select
          aria-label="Padding Format"
          className="rounded border border-slate-300 px-3 py-2 text-sm font-normal"
          value={values.paddingFormat}
          onChange={(e) => set("paddingFormat", e.target.value)}
        >
          {paddingOptions.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
      </label>

      <label className="flex items-center gap-2 text-sm font-semibold">
        <input
          type="checkbox"
          checked={values.replaceSpaces}
          onChange={(e) => set("replaceSpaces", e.target.checked)}
        />
        Replace Spaces
      </label>

      <label className="flex items-center gap-2 text-sm font-semibold">
        <input
          type="checkbox"
          checked={values.lowercase}
          onChange={(e) => set("lowercase", e.target.checked)}
        />
        Lowercase filename
      </label>

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
          Save renaming settings
        </button>
      </div>
    </form>
  );
}
