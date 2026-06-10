import { useState } from "react";

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

// DownloadClientForm is the functional-minimal edit form for the SABnzbd download client,
// mirroring IndexerForm. The API key input is masked (type="password") and is never seeded
// with the stored key — on edit the field starts blank and a blank submit leaves the
// stored key unchanged server-side (T-ul5-01).
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
      className="flex flex-col gap-3 rounded border border-slate-200 bg-white p-4"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit(values);
      }}
    >
      <label className="flex flex-col gap-1 text-sm font-semibold">
        URL
        <input
          className="rounded border border-slate-300 px-3 py-2 text-sm font-normal"
          value={values.url}
          onChange={(e) => set("url", e.target.value)}
          required
        />
      </label>

      <label className="flex flex-col gap-1 text-sm font-semibold">
        API key
        <input
          type="password"
          aria-label="API key"
          placeholder="Leave blank to keep current key"
          className="rounded border border-slate-300 px-3 py-2 text-sm font-normal"
          value={values.apiKey}
          onChange={(e) => set("apiKey", e.target.value)}
        />
      </label>

      <label className="flex flex-col gap-1 text-sm font-semibold">
        Category
        <input
          className="rounded border border-slate-300 px-3 py-2 text-sm font-normal"
          placeholder="Defaults to comics"
          value={values.category}
          onChange={(e) => set("category", e.target.value)}
        />
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
          Save download client
        </button>
      </div>
    </form>
  );
}
