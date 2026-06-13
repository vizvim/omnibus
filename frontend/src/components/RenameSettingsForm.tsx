import { useState } from "react";

import { Button } from "./ui/button";
import { Input } from "./ui/input";

// RenameSettingsFormValues mirrors the RenameConfig proto message one-to-one (D-09).
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

const fieldLabel = "flex flex-col gap-1.5 text-sm font-medium text-foreground";
const checkRow = "flex items-center gap-2.5 text-sm font-medium text-foreground";
const checkboxClass = "size-4 shrink-0 rounded border-input accent-[#7aa2f7]";
const selectClass =
  "h-9 rounded-md border border-input bg-tn-night/60 px-3 text-sm text-foreground transition-colors focus-visible:border-primary/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40";

// RenameSettingsForm is the edit form for the renaming config (D-09): the Rename master
// toggle, folder/file format templates, Replace Spaces, Lowercase, and the Issue Number
// Padding toggle + format dropdown.
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
      className="flex flex-col gap-4 rounded-xl border border-border bg-card/70 p-5"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit(values);
      }}
    >
      <label className={checkRow}>
        <input
          type="checkbox"
          className={checkboxClass}
          checked={values.renameEnabled}
          onChange={(e) => set("renameEnabled", e.target.checked)}
        />
        Rename files
      </label>

      <label className={fieldLabel}>
        Folder Format
        <Input
          value={values.folderFormat}
          onChange={(e) => set("folderFormat", e.target.value)}
          placeholder="$Publisher/$Series ($Year)"
          className="font-mono"
        />
      </label>

      <label className={fieldLabel}>
        File Format
        <Input
          value={values.fileFormat}
          onChange={(e) => set("fileFormat", e.target.value)}
          placeholder="$Series $VolumeY $Annual #$Issue ($monthname $Year)"
          className="font-mono"
        />
      </label>

      <label className={checkRow}>
        <input
          type="checkbox"
          className={checkboxClass}
          checked={values.issuePaddingEnabled}
          onChange={(e) => set("issuePaddingEnabled", e.target.checked)}
        />
        Issue Number Padding
      </label>

      <label className={fieldLabel}>
        Padding Format
        <select
          aria-label="Padding Format"
          className={selectClass}
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

      <label className={checkRow}>
        <input
          type="checkbox"
          className={checkboxClass}
          checked={values.replaceSpaces}
          onChange={(e) => set("replaceSpaces", e.target.checked)}
        />
        Replace Spaces
      </label>

      <label className={checkRow}>
        <input
          type="checkbox"
          className={checkboxClass}
          checked={values.lowercase}
          onChange={(e) => set("lowercase", e.target.checked)}
        />
        Lowercase filename
      </label>

      {error ? <p className="text-sm text-tn-red">{error}</p> : null}

      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending}>
          Save renaming settings
        </Button>
      </div>
    </form>
  );
}
