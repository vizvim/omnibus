import { useState } from "react";

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

// IndexerForm is the functional-minimal add/edit form (native inputs at this bar). The
// API key input is masked (type="password") per UI-SPEC / T-4-01 — the stored key is
// never rendered, so on edit the field starts blank and an empty submit leaves it
// unchanged server-side.
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
      className="flex flex-col gap-3 rounded border border-slate-200 bg-white p-4"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit(values);
      }}
    >
      <label className="flex flex-col gap-1 text-sm font-semibold">
        Name
        <input
          className="rounded border border-slate-300 px-3 py-2 text-sm font-normal"
          value={values.name}
          onChange={(e) => set("name", e.target.value)}
          required
        />
      </label>

      <label className="flex flex-col gap-1 text-sm font-semibold">
        Kind
        <select
          className="rounded border border-slate-300 px-3 py-2 text-sm font-normal"
          value={values.kind}
          onChange={(e) => set("kind", e.target.value)}
        >
          <option value="newznab">Newznab</option>
          <option value="getcomics">GetComics</option>
        </select>
      </label>

      <label className="flex flex-col gap-1 text-sm font-semibold">
        Base URL
        <input
          className="rounded border border-slate-300 px-3 py-2 text-sm font-normal"
          value={values.baseUrl}
          onChange={(e) => set("baseUrl", e.target.value)}
          required
        />
      </label>

      <label className="flex flex-col gap-1 text-sm font-semibold">
        API key
        <input
          type="password"
          aria-label="API key"
          placeholder={editing ? "Leave blank to keep current key" : ""}
          className="rounded border border-slate-300 px-3 py-2 text-sm font-normal"
          value={values.apiKey}
          onChange={(e) => set("apiKey", e.target.value)}
        />
      </label>

      <label className="flex flex-col gap-1 text-sm font-semibold">
        Categories
        <input
          className="rounded border border-slate-300 px-3 py-2 text-sm font-normal"
          placeholder="Defaults to 7030 for Newznab"
          value={values.categories}
          onChange={(e) => set("categories", e.target.value)}
        />
      </label>

      <label className="flex items-center gap-2 text-sm font-semibold">
        <input
          type="checkbox"
          checked={values.useForRss}
          onChange={(e) => set("useForRss", e.target.checked)}
        />
        Use for RSS
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
          {editing ? "Save indexer" : "Add indexer"}
        </button>
      </div>
    </form>
  );
}
