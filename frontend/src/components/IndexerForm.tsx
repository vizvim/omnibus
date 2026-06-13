import { useState } from "react";

import { Button } from "./ui/button";
import { Input } from "./ui/input";

// IndexerFormValues is the editable indexer payload the form collects.
export interface IndexerFormValues {
  name: string;
  kind: string;
  baseUrl: string;
  apiKey: string;
  categories: string;
  useForRss: boolean;
}

const emptyValues: IndexerFormValues = {
  name: "",
  kind: "newznab",
  baseUrl: "",
  apiKey: "",
  categories: "",
  useForRss: true,
};

const fieldLabel = "flex flex-col gap-1.5 text-sm font-medium text-foreground";
const selectClass =
  "h-9 rounded-md border border-input bg-tn-night/60 px-3 text-sm text-foreground transition-colors focus-visible:border-primary/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/40";
const checkboxClass = "size-4 shrink-0 rounded border-input accent-[#7aa2f7]";

// IndexerForm is the add/edit form. The API key input is masked (type="password") per
// T-4-01 — the stored key is never rendered, so on edit the field starts blank and an
// empty submit leaves it unchanged server-side.
export function IndexerForm({
  initial,
  editing = false,
  pending = false,
  error,
  onSubmit,
  onCancel,
}: {
  initial?: Partial<IndexerFormValues>;
  editing?: boolean;
  pending?: boolean;
  error?: string;
  onSubmit: (values: IndexerFormValues) => void;
  onCancel: () => void;
}) {
  const [values, setValues] = useState<IndexerFormValues>({
    ...emptyValues,
    ...initial,
    apiKey: "", // never seed the masked field with a stored key
  });

  function set<K extends keyof IndexerFormValues>(key: K, value: IndexerFormValues[K]) {
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
        Name
        <Input value={values.name} onChange={(e) => set("name", e.target.value)} required />
      </label>

      <label className={fieldLabel}>
        Kind
        <select
          className={selectClass}
          value={values.kind}
          onChange={(e) => set("kind", e.target.value)}
        >
          <option value="newznab">Newznab</option>
        </select>
      </label>

      <label className={fieldLabel}>
        Base URL
        <Input
          value={values.baseUrl}
          onChange={(e) => set("baseUrl", e.target.value)}
          required
        />
      </label>

      <label className={fieldLabel}>
        API key
        <Input
          type="password"
          aria-label="API key"
          placeholder={editing ? "Leave blank to keep current key" : ""}
          value={values.apiKey}
          onChange={(e) => set("apiKey", e.target.value)}
        />
      </label>

      <label className={fieldLabel}>
        Categories
        <Input
          placeholder="Defaults to 7030 for Newznab"
          value={values.categories}
          onChange={(e) => set("categories", e.target.value)}
        />
      </label>

      <label className="flex items-center gap-2.5 text-sm font-medium text-foreground">
        <input
          type="checkbox"
          className={checkboxClass}
          checked={values.useForRss}
          onChange={(e) => set("useForRss", e.target.checked)}
        />
        Use for RSS
      </label>

      {error ? <p className="text-sm text-tn-red">{error}</p> : null}

      <div className="flex justify-end gap-2">
        <Button type="button" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={pending}>
          {editing ? "Save indexer" : "Add indexer"}
        </Button>
      </div>
    </form>
  );
}
