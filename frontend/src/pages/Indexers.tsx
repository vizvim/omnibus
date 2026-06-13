import { useMutation, useQuery } from "@connectrpc/connect-query";
import { CheckCircle2, Plus, Radio, XCircle } from "lucide-react";
import { useState } from "react";

import { AddConfirmDialog } from "../components/AddConfirmDialog";
import { EmptyState } from "../components/EmptyState";
import { IndexerForm, type IndexerFormValues } from "../components/IndexerForm";
import { PageHeader } from "../components/layout/PageHeader";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
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

// Indexers is the indexer-management page (SRCH-09, D-16): a list of DB-backed indexers
// with add/edit/enable-disable/delete and a per-row connectivity probe.
export function Indexers() {
  const list = useQuery(listIndexers, {}, { retry: false });
  const [formMode, setFormMode] = useState<"closed" | "add" | { edit: Indexer }>("closed");
  const [pendingDelete, setPendingDelete] = useState<Indexer | undefined>(undefined);
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
      <div className="flex flex-col gap-8">
        <PageHeader eyebrow="Sources" title="Indexers" />
        <EmptyState
          icon={<Radio />}
          heading="Couldn't load indexers"
          body="Couldn't reach the server. Check your connection, then try again."
        />
      </div>
    );
  }

  const indexers = list.data?.indexers ?? [];

  return (
    <div className="flex flex-col gap-8">
      <PageHeader
        eyebrow="Sources"
        title="Indexers"
        description="omnibus searches every enabled Newznab indexer for releases."
        actions={
          formMode === "closed" ? (
            <Button onClick={() => setFormMode("add")} className="gap-1.5">
              <Plus className="size-4" />
              Add indexer
            </Button>
          ) : undefined
        }
      />

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
          icon={<Radio />}
          heading="No indexers configured"
          body="Add a Newznab indexer to start finding releases. omnibus searches every enabled indexer."
          cta={
            <Button onClick={() => setFormMode("add")} className="gap-1.5">
              <Plus className="size-4" />
              Add indexer
            </Button>
          }
        />
      ) : (
        <ul className="flex flex-col gap-2">
          {indexers.map((idx) => {
            const result = testResults[idx.id.toString()];
            return (
              <li
                key={idx.id.toString()}
                className="flex flex-wrap items-center justify-between gap-4 rounded-xl border border-border bg-card/60 p-4 transition-colors hover:border-border-strong/70"
              >
                <div className="flex flex-col gap-1.5">
                  <div className="flex items-center gap-2.5">
                    <span className="text-sm font-medium text-foreground">{idx.name}</span>
                    <Badge variant={idx.enabled ? "success" : "neutral"} dot>
                      {idx.enabled ? "Enabled" : "Disabled"}
                    </Badge>
                  </div>
                  <span className="font-mono text-xs text-muted-foreground">
                    {idx.kind} · {idx.baseUrl}
                  </span>
                  {result ? (
                    result.ok ? (
                      <span className="inline-flex items-center gap-1.5 text-xs font-medium text-tn-green">
                        <CheckCircle2 className="size-3.5" /> Connected
                      </span>
                    ) : (
                      <span className="inline-flex items-center gap-1.5 text-xs font-medium text-tn-red">
                        <XCircle className="size-3.5" /> {result.detail}
                      </span>
                    )
                  ) : null}
                </div>
                <div className="flex items-center gap-2">
                  <Button
                    variant="secondary"
                    size="sm"
                    onClick={() => runTest(idx)}
                    disabled={test.isPending && testingId === idx.id.toString()}
                  >
                    Test
                  </Button>
                  <Button variant="secondary" size="sm" onClick={() => toggleEnabled(idx)}>
                    {idx.enabled ? "Disable" : "Enable"}
                  </Button>
                  <Button variant="secondary" size="sm" onClick={() => setFormMode({ edit: idx })}>
                    Edit
                  </Button>
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
                        Delete “{idx.name}”? Targeted search and RSS will stop using it. This
                        can't be undone.
                      </>
                    }
                    onConfirm={() => remove.mutate({ id: idx.id })}
                    trigger={
                      <Button variant="destructive" size="sm">
                        Delete
                      </Button>
                    }
                  />
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
