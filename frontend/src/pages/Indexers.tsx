import { useMutation, useQuery } from "@connectrpc/connect-query";
import { useState } from "react";

import { AddConfirmDialog } from "../components/AddConfirmDialog";
import { EmptyState } from "../components/EmptyState";
import { IndexerForm, type IndexerFormValues } from "../components/IndexerForm";
import type { Indexer } from "../gen/omnibus/v1/indexers_pb";
import {
  createIndexer,
  deleteIndexer,
  listIndexers,
  testIndexer,
  updateIndexer,
} from "../gen/omnibus/v1/indexers-IndexerService_connectquery";

type TestOutcome = { ok: boolean; detail: string };

const SAVE_ERROR = "Couldn't save the indexer. Check the URL and API key, then try again.";

// Indexers is the functional-minimal indexer-management page (SRCH-09, D-16): a
// single-column list of DB-backed indexers with add/edit/enable-disable/delete. It
// reuses EmptyState, AddConfirmDialog (for delete), and the Activity list-row styling.
export function Indexers() {
  const list = useQuery(listIndexers, {}, { retry: false });
  const [formMode, setFormMode] = useState<"closed" | "add" | { edit: Indexer }>("closed");
  const [pendingDelete, setPendingDelete] = useState<Indexer | undefined>(undefined);
  // Per-row connectivity-probe outcome, keyed by indexer id. The in-flight id is captured
  // before mutate so onSuccess can attribute the result to the right row.
  const [testResults, setTestResults] = useState<Record<string, TestOutcome>>({});
  const [testingId, setTestingId] = useState<string | undefined>(undefined);

  const refetch = () => void list.refetch();

  const create = useMutation(createIndexer, {
    onSuccess: () => {
      setFormMode("closed");
      refetch();
    },
  });
  const update = useMutation(updateIndexer, {
    onSuccess: () => {
      setFormMode("closed");
      refetch();
    },
  });
  const remove = useMutation(deleteIndexer, {
    onSuccess: () => {
      setPendingDelete(undefined);
      refetch();
    },
  });
  const test = useMutation(testIndexer, {
    onSuccess: (res, vars) => {
      const id = (vars?.id ?? testingId ?? "").toString();
      if (id) {
        setTestResults((prev) => ({ ...prev, [id]: { ok: res.ok, detail: res.detail } }));
      }
    },
  });

  function runTest(target: Indexer) {
    setTestingId(target.id.toString());
    test.mutate({ id: target.id });
  }

  function submitAdd(values: IndexerFormValues) {
    create.mutate({
      name: values.name,
      kind: values.kind,
      baseUrl: values.baseUrl,
      apiKey: values.apiKey,
      categories: values.categories,
      useForRss: values.useForRss,
    });
  }

  function submitEdit(target: Indexer, values: IndexerFormValues) {
    update.mutate({
      id: target.id,
      name: values.name,
      kind: values.kind,
      baseUrl: values.baseUrl,
      apiKey: values.apiKey, // empty leaves the stored key unchanged
      enabled: target.enabled,
      categories: values.categories,
      useForRss: values.useForRss,
    });
  }

  function toggleEnabled(target: Indexer) {
    update.mutate({
      id: target.id,
      name: target.name,
      kind: target.kind,
      baseUrl: target.baseUrl,
      apiKey: "", // keep stored key
      enabled: !target.enabled,
      categories: target.categories,
      useForRss: target.useForRss,
    });
  }

  if (list.isError) {
    return (
      <EmptyState
        heading="Couldn't load indexers"
        body="Couldn't reach the server. Check your connection, then try again."
      />
    );
  }

  const indexers = list.data?.indexers ?? [];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center justify-between">
        <h1 className="text-2xl font-semibold">Indexers</h1>
        {formMode === "closed" ? (
          <button
            type="button"
            onClick={() => setFormMode("add")}
            className="rounded bg-blue-600 px-4 py-2 text-sm font-semibold text-white"
          >
            Add indexer
          </button>
        ) : null}
      </div>

      {formMode === "add" ? (
        <IndexerForm
          pending={create.isPending}
          error={create.isError ? SAVE_ERROR : undefined}
          onSubmit={submitAdd}
          onCancel={() => setFormMode("closed")}
        />
      ) : null}

      {typeof formMode === "object" ? (
        <IndexerForm
          editing
          initial={{
            name: formMode.edit.name,
            kind: formMode.edit.kind,
            baseUrl: formMode.edit.baseUrl,
            categories: formMode.edit.categories,
            useForRss: formMode.edit.useForRss,
          }}
          pending={update.isPending}
          error={update.isError ? SAVE_ERROR : undefined}
          onSubmit={(values) => submitEdit(formMode.edit, values)}
          onCancel={() => setFormMode("closed")}
        />
      ) : null}

      {indexers.length === 0 && formMode === "closed" ? (
        <EmptyState
          heading="No indexers configured"
          body="Add a Newznab indexer to start finding releases. omnibus searches every enabled indexer."
          cta={
            <button
              type="button"
              onClick={() => setFormMode("add")}
              className="rounded bg-blue-600 px-4 py-2 text-sm font-semibold text-white"
            >
              Add indexer
            </button>
          }
        />
      ) : (
        <ul className="flex flex-col gap-2">
          {indexers.map((idx) => (
            <li
              key={idx.id.toString()}
              className="flex items-center justify-between gap-4 rounded border border-slate-200 bg-slate-50 p-4"
            >
              <div className="flex flex-col gap-1">
                <div className="flex items-center gap-2">
                  <span className="text-base">{idx.name}</span>
                  <span
                    className={`inline-flex items-center rounded px-2 py-0.5 text-sm font-semibold ${
                      idx.enabled ? "bg-green-100 text-green-700" : "bg-slate-100 text-slate-500"
                    }`}
                  >
                    {idx.enabled ? "Enabled" : "Disabled"}
                  </span>
                </div>
                <span className="text-sm text-slate-600">
                  {idx.kind} · {idx.baseUrl}
                </span>
                {testResults[idx.id.toString()] ? (
                  testResults[idx.id.toString()].ok ? (
                    <span className="text-sm font-semibold text-green-700">✓ Connected</span>
                  ) : (
                    <span className="text-sm font-semibold text-red-600">
                      ✗ {testResults[idx.id.toString()].detail}
                    </span>
                  )
                ) : null}
              </div>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => runTest(idx)}
                  disabled={test.isPending && testingId === idx.id.toString()}
                  className="rounded bg-slate-100 px-3 py-1.5 text-sm font-semibold"
                >
                  Test
                </button>
                <button
                  type="button"
                  onClick={() => toggleEnabled(idx)}
                  className="rounded bg-slate-100 px-3 py-1.5 text-sm font-semibold"
                >
                  {idx.enabled ? "Disable" : "Enable"}
                </button>
                <button
                  type="button"
                  onClick={() => setFormMode({ edit: idx })}
                  className="rounded bg-slate-100 px-3 py-1.5 text-sm font-semibold"
                >
                  Edit
                </button>
                <AddConfirmDialog
                  open={pendingDelete?.id === idx.id}
                  onOpenChange={(open) => setPendingDelete(open ? idx : undefined)}
                  pending={remove.isPending}
                  seriesName={idx.name}
                  destructive
                  title="Delete indexer"
                  confirmLabel="Delete"
                  description={
                    <>
                      Delete “{idx.name}”? Targeted search and RSS will stop using it. This can't be
                      undone.
                    </>
                  }
                  onConfirm={() => remove.mutate({ id: idx.id })}
                  trigger={
                    <button
                      type="button"
                      className="rounded bg-red-600 px-3 py-1.5 text-sm font-semibold text-white"
                    >
                      Delete
                    </button>
                  }
                />
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
