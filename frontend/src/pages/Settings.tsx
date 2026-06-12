import { useMutation, useQuery } from "@connectrpc/connect-query";
import { useState } from "react";

import {
  DownloadClientForm,
  type DownloadClientFormValues,
} from "../components/DownloadClientForm";
import { EmptyState } from "../components/EmptyState";
import {
  getDownloadClientConfig,
  testDownloadClientConfig,
  updateDownloadClientConfig,
} from "../gen/omnibus/v1/download_client-DownloadClientService_connectquery";

const SAVE_ERROR = "Couldn't save the download client. Check the URL and API key, then try again.";

// Settings is the functional-minimal settings page. It currently hosts the Download
// Client (SABnzbd) section: the DB-backed config is shown (URL + category, never the API
// key) with an edit affordance. Saving via DownloadClientService persists at runtime.
export function Settings() {
  const config = useQuery(getDownloadClientConfig, {}, { retry: false });
  const [editing, setEditing] = useState(false);
  const [testResult, setTestResult] = useState<{ ok: boolean; detail: string } | undefined>();

  const update = useMutation(updateDownloadClientConfig, {
    onSuccess: () => {
      setEditing(false);
      void config.refetch();
    },
  });
  const test = useMutation(testDownloadClientConfig, {
    onSuccess: (res) => setTestResult({ ok: res.ok, detail: res.detail }),
  });

  function submitEdit(values: DownloadClientFormValues) {
    update.mutate({
      url: values.url,
      apiKey: values.apiKey, // empty leaves the stored key unchanged
      category: values.category,
    });
  }

  if (config.isError) {
    return (
      <EmptyState
        heading="Couldn't load settings"
        body="Couldn't reach the server. Check your connection, then try again."
      />
    );
  }

  const current = config.data?.config;

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl font-semibold">Settings</h1>

      <section className="flex flex-col gap-4">
        <div className="flex items-center justify-between">
          <h2 className="text-xl font-semibold">Download Client</h2>
          {!editing ? (
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => test.mutate({})}
                disabled={test.isPending}
                className="rounded bg-slate-100 px-4 py-2 text-sm font-semibold"
              >
                Test connection
              </button>
              <button
                type="button"
                onClick={() => setEditing(true)}
                className="rounded bg-blue-600 px-4 py-2 text-sm font-semibold text-white"
              >
                Edit
              </button>
            </div>
          ) : null}
        </div>

        {editing ? (
          <DownloadClientForm
            initial={{ url: current?.url ?? "", category: current?.category ?? "comics" }}
            pending={update.isPending}
            error={update.isError ? SAVE_ERROR : undefined}
            onSubmit={submitEdit}
            onCancel={() => setEditing(false)}
          />
        ) : (
          <div className="flex flex-col gap-2 rounded border border-slate-200 bg-slate-50 p-4">
            <div className="flex items-center gap-2">
              <span className="text-base">SABnzbd</span>
              <span
                className={`inline-flex items-center rounded px-2 py-0.5 text-sm font-semibold ${
                  current?.configured
                    ? "bg-green-100 text-green-700"
                    : "bg-slate-100 text-slate-500"
                }`}
              >
                {current?.configured ? "Configured" : "Not configured"}
              </span>
            </div>
            <span className="text-sm text-slate-600">
              URL: <span>{current?.url || "—"}</span>
            </span>
            <span className="text-sm text-slate-600">
              Category: <span>{current?.category || "comics"}</span>
            </span>
            {testResult ? (
              testResult.ok ? (
                <span className="text-sm font-semibold text-green-700">✓ Connected</span>
              ) : (
                <span className="text-sm font-semibold text-red-600">✗ {testResult.detail}</span>
              )
            ) : null}
          </div>
        )}
      </section>
    </div>
  );
}
