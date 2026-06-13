import { useState } from "react";

import { Button } from "./ui/button";
import { Input } from "./ui/input";

// DownloadClientFormValues is the editable download-client payload the form collects.
export interface DownloadClientFormValues {
  url: string;
  apiKey: string;
  category: string;
}

const emptyValues: DownloadClientFormValues = {
  url: "",
  apiKey: "",
  category: "comics",
};

const fieldLabel = "flex flex-col gap-1.5 text-sm font-medium text-foreground";

// DownloadClientForm is the edit form for the SABnzbd download client. The API key input is
// masked (type="password") and never seeded with the stored key — on edit the field starts
// blank and a blank submit leaves the stored key unchanged server-side (T-ul5-01).
export function DownloadClientForm({
  initial,
  pending = false,
  error,
  onSubmit,
  onCancel,
}: {
  initial?: Partial<Pick<DownloadClientFormValues, "url" | "category">>;
  pending?: boolean;
  error?: string;
  onSubmit: (values: DownloadClientFormValues) => void;
  onCancel: () => void;
}) {
  const [values, setValues] = useState<DownloadClientFormValues>({
    ...emptyValues,
    ...initial,
    apiKey: "", // never seed the masked field with a stored key
  });

  function set<K extends keyof DownloadClientFormValues>(
    key: K,
    value: DownloadClientFormValues[K],
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
        URL
        <Input value={values.url} onChange={(e) => set("url", e.target.value)} required />
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

      <label className={fieldLabel}>
        Category
        <Input
          placeholder="Defaults to comics"
          value={values.category}
          onChange={(e) => set("category", e.target.value)}
        />
      </label>

      {error ? <p className="text-sm text-tn-red">{error}</p> : null}

      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending}>
          Save download client
        </Button>
      </div>
    </form>
  );
}
