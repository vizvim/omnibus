import { useState } from "react";

import { Button } from "./ui/button";
import { Input } from "./ui/input";

// MetadataSettingsFormValues is the editable metadata-provider payload the form collects.
// provider is fixed to "comicvine" today; the field is kept so additional providers can be
// added later.
export interface MetadataSettingsFormValues {
  apiKey: string;
}

const emptyValues: MetadataSettingsFormValues = {
  apiKey: "",
};

const fieldLabel = "flex flex-col gap-1.5 text-sm font-medium text-foreground";

// MetadataSettingsForm is the edit form for the ComicVine metadata provider. The API key
// input is masked (type="password") and never seeded with the stored key — on edit the
// field starts blank and a blank submit leaves the stored key unchanged server-side
// (mirrors DownloadClientForm / T-ul5-01). The provider is shown as a read-only label.
export function MetadataSettingsForm({
  pending = false,
  error,
  onSubmit,
  onCancel,
}: {
  pending?: boolean;
  error?: string;
  onSubmit: (values: MetadataSettingsFormValues) => void;
  onCancel: () => void;
}) {
  const [values, setValues] = useState<MetadataSettingsFormValues>({
    ...emptyValues,
    apiKey: "", // never seed the masked field with a stored key
  });

  function set<K extends keyof MetadataSettingsFormValues>(
    key: K,
    value: MetadataSettingsFormValues[K],
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
      <label className={fieldLabel}>
        Provider
        <select
          aria-label="Provider"
          disabled
          value="comicvine"
          className="flex h-10 w-full rounded-md border border-border bg-background px-3 py-2 text-sm text-foreground disabled:cursor-not-allowed disabled:opacity-70"
        >
          <option value="comicvine">ComicVine</option>
        </select>
      </label>

      <label className={fieldLabel}>
        API key
        <Input
          type="password"
          aria-label="API key"
          placeholder="Leave blank to keep current key"
          value={values.apiKey}
          onChange={(e) => set("apiKey", e.target.value)}
        />
      </label>

      {error ? <p className="text-sm text-tn-red">{error}</p> : null}

      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending}>
          Save metadata provider
        </Button>
      </div>
    </form>
  );
}
