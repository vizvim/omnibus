import { useMutation, useQuery } from "@connectrpc/connect-query";
import { CheckCircle2, Pencil, XCircle } from "lucide-react";
import type { ReactNode } from "react";
import { useState } from "react";

import {
  AuthSettingsForm,
  type AuthSettingsFormValues,
} from "../components/AuthSettingsForm";
import {
  DDLSettingsForm,
  type DDLSettingsFormValues,
} from "../components/DDLSettingsForm";
import {
  DownloadClientForm,
  type DownloadClientFormValues,
} from "../components/DownloadClientForm";
import { EmptyState } from "../components/EmptyState";
import { PageHeader } from "../components/layout/PageHeader";
import {
  MetadataSettingsForm,
  type MetadataSettingsFormValues,
} from "../components/MetadataSettingsForm";
import {
  RenameSettingsForm,
  type RenameSettingsFormValues,
} from "../components/RenameSettingsForm";
import { Badge } from "../components/ui/badge";
import { Button } from "../components/ui/button";
import {
  getAuthConfig,
  updateAuthConfig,
} from "../gen/omnibus/v1/auth-AuthService_connectquery";
import { AuthMode } from "../gen/omnibus/v1/auth_pb";
import {
  getDDLConfig,
  updateDDLConfig,
} from "../gen/omnibus/v1/ddl_config-DDLConfigService_connectquery";
import {
  getDownloadClientConfig,
  testDownloadClientConfig,
  updateDownloadClientConfig,
} from "../gen/omnibus/v1/download_client-DownloadClientService_connectquery";
import {
  getMetadataProviderConfig,
  testMetadataProviderConfig,
  updateMetadataProviderConfig,
} from "../gen/omnibus/v1/metadata_provider-MetadataProviderService_connectquery";
import {
  getRenameConfig,
  updateRenameConfig,
} from "../gen/omnibus/v1/rename_config-RenameConfigService_connectquery";

const SAVE_ERROR = "Couldn't save the download client. Check the URL and API key, then try again.";
const METADATA_SAVE_ERROR =
  "Couldn't save the metadata provider. Check the API key, then try again.";
const RENAME_SAVE_ERROR =
  "Couldn't save the renaming settings. Check the folder and file formats, then try again.";
const DDL_SAVE_ERROR = "Couldn't save the DDL settings. Try again.";
const AUTH_SAVE_ERROR =
  "Couldn't save the authentication settings. Check the username and password, then try again.";

// authModeLabel maps the AuthMode enum to its locked read-view label (UI-SPEC Copywriting).
function authModeLabel(mode: AuthMode | undefined): string {
  switch (mode) {
    case AuthMode.ON:
      return "On";
    case AuthMode.BYPASS_LOCAL:
      return "On, except local addresses";
    default:
      return "Off";
  }
}

// SettingsCard is the shared section frame: a titled card with a right-aligned actions slot.
function SettingsCard({
  title,
  description,
  actions,
  children,
}: {
  title: string;
  description?: string;
  actions?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="surface-sheen flex flex-col gap-4 rounded-xl border border-border bg-card/60 p-5">
      <div className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-0.5">
          <h2 className="font-display text-lg font-semibold tracking-tight text-foreground">
            {title}
          </h2>
          {description ? <p className="text-sm text-muted-foreground">{description}</p> : null}
        </div>
        {actions ? <div className="flex shrink-0 items-center gap-2">{actions}</div> : null}
      </div>
      {children}
    </section>
  );
}

// ReadRow is one read-only label/value line. The value stays in its own element so tests
// can select it by exact text.
function ReadRow({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-4 border-t border-border/60 py-2 text-sm first:border-t-0">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-mono text-foreground">{children}</span>
    </div>
  );
}

// Settings hosts the Download Client (SABnzbd), Renaming, and Direct Downloads sections.
// Each is DB-backed and runtime-editable.
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

  const metadataConfig = useQuery(getMetadataProviderConfig, {}, { retry: false });
  const [editingMetadata, setEditingMetadata] = useState(false);
  const [metadataTestResult, setMetadataTestResult] = useState<
    { ok: boolean; detail: string } | undefined
  >();
  const metadataUpdate = useMutation(updateMetadataProviderConfig, {
    onSuccess: () => {
      setEditingMetadata(false);
      void metadataConfig.refetch();
    },
  });
  const metadataTest = useMutation(testMetadataProviderConfig, {
    onSuccess: (res) => setMetadataTestResult({ ok: res.ok, detail: res.detail }),
  });

  const renameConfig = useQuery(getRenameConfig, {}, { retry: false });
  const [editingRename, setEditingRename] = useState(false);
  const renameUpdate = useMutation(updateRenameConfig, {
    onSuccess: () => {
      setEditingRename(false);
      void renameConfig.refetch();
    },
  });

  const ddlConfig = useQuery(getDDLConfig, {}, { retry: false });
  const [editingDDL, setEditingDDL] = useState(false);
  const ddlUpdate = useMutation(updateDDLConfig, {
    onSuccess: () => {
      setEditingDDL(false);
      void ddlConfig.refetch();
    },
  });

  const authConfig = useQuery(getAuthConfig, {}, { retry: false });
  const [editingAuth, setEditingAuth] = useState(false);
  const authUpdate = useMutation(updateAuthConfig, {
    onSuccess: () => {
      setEditingAuth(false);
      void authConfig.refetch();
    },
  });

  function submitEdit(values: DownloadClientFormValues) {
    update.mutate({
      url: values.url,
      apiKey: values.apiKey, // empty leaves the stored key unchanged
      category: values.category,
    });
  }

  function submitMetadataEdit(values: MetadataSettingsFormValues) {
    metadataUpdate.mutate({
      provider: "comicvine",
      apiKey: values.apiKey, // empty leaves the stored key unchanged
    });
  }

  function submitRenameEdit(values: RenameSettingsFormValues) {
    renameUpdate.mutate({
      folderFormat: values.folderFormat,
      fileFormat: values.fileFormat,
      renameEnabled: values.renameEnabled,
      issuePaddingEnabled: values.issuePaddingEnabled,
      paddingFormat: values.paddingFormat,
      replaceSpaces: values.replaceSpaces,
      lowercase: values.lowercase,
    });
  }

  function submitDDLEdit(values: DDLSettingsFormValues) {
    ddlUpdate.mutate({ enabled: values.enabled });
  }

  function submitAuthEdit(values: AuthSettingsFormValues) {
    authUpdate.mutate({
      mode: values.mode,
      username: values.username,
      password: values.password, // empty leaves the stored credential unchanged (D-02)
    });
  }

  if (config.isError) {
    return (
      <div className="flex flex-col gap-8">
        <PageHeader eyebrow="Configuration" title="Settings" />
        <EmptyState
          heading="Couldn't load settings"
          body="Couldn't reach the server. Check your connection, then try again."
        />
      </div>
    );
  }

  const current = config.data?.config;
  const metadataCurrent = metadataConfig.data?.config;
  const renameCurrent = renameConfig.data?.config;
  const ddlCurrent = ddlConfig.data?.config;
  const authCurrent = authConfig.data?.config;

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        eyebrow="Configuration"
        title="Settings"
        description="Download client, file renaming, and direct-download fallback."
      />

      {/* Download Client */}
      <SettingsCard
        title="Download Client"
        description="Where grabbed releases are handed off."
        actions={
          !editing ? (
            <>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => test.mutate({})}
                disabled={test.isPending}
              >
                Test connection
              </Button>
              <Button
                size="sm"
                aria-label="Edit download client"
                onClick={() => setEditing(true)}
                className="gap-1.5"
              >
                <Pencil className="size-3.5" />
                Edit
              </Button>
            </>
          ) : undefined
        }
      >
        {editing ? (
          <DownloadClientForm
            initial={{ url: current?.url ?? "", category: current?.category ?? "comics" }}
            pending={update.isPending}
            error={update.isError ? SAVE_ERROR : undefined}
            onSubmit={submitEdit}
            onCancel={() => setEditing(false)}
          />
        ) : (
          <div className="rounded-lg border border-border bg-tn-night/30 p-4">
            <div className="flex items-center gap-2.5 pb-2">
              <span className="text-sm font-medium text-foreground">SABnzbd</span>
              <Badge variant={current?.configured ? "success" : "neutral"} dot>
                {current?.configured ? "Configured" : "Not configured"}
              </Badge>
            </div>
            <ReadRow label="URL">{current?.url || "—"}</ReadRow>
            <ReadRow label="Category">{current?.category || "comics"}</ReadRow>
            {testResult ? (
              testResult.ok ? (
                <p className="inline-flex items-center gap-1.5 pt-2 text-sm font-medium text-tn-green">
                  <CheckCircle2 className="size-4" /> Connected
                </p>
              ) : (
                <p className="inline-flex items-center gap-1.5 pt-2 text-sm font-medium text-tn-red">
                  <XCircle className="size-4" /> {testResult.detail}
                </p>
              )
            ) : null}
          </div>
        )}
      </SettingsCard>

      {/* Metadata */}
      <SettingsCard
        title="Metadata"
        description="Where series and issue metadata is sourced from."
        actions={
          !editingMetadata && !metadataConfig.isError ? (
            <>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => metadataTest.mutate({})}
                disabled={metadataTest.isPending}
              >
                Test connection
              </Button>
              <Button
                size="sm"
                aria-label="Edit metadata provider"
                onClick={() => setEditingMetadata(true)}
                className="gap-1.5"
              >
                <Pencil className="size-3.5" />
                Edit
              </Button>
            </>
          ) : undefined
        }
      >
        {metadataConfig.isError ? (
          <p className="text-sm text-tn-red">Couldn't load the metadata settings.</p>
        ) : editingMetadata ? (
          <MetadataSettingsForm
            pending={metadataUpdate.isPending}
            error={metadataUpdate.isError ? METADATA_SAVE_ERROR : undefined}
            onSubmit={submitMetadataEdit}
            onCancel={() => setEditingMetadata(false)}
          />
        ) : (
          <div className="rounded-lg border border-border bg-tn-night/30 p-4">
            <div className="flex items-center gap-2.5 pb-2">
              <span className="text-sm font-medium text-foreground">ComicVine</span>
              <Badge variant={metadataCurrent?.configured ? "success" : "neutral"} dot>
                {metadataCurrent?.configured ? "Configured" : "Not configured"}
              </Badge>
            </div>
            <ReadRow label="Provider">ComicVine</ReadRow>
            {metadataTestResult ? (
              metadataTestResult.ok ? (
                <p className="inline-flex items-center gap-1.5 pt-2 text-sm font-medium text-tn-green">
                  <CheckCircle2 className="size-4" /> Connected
                </p>
              ) : (
                <p className="inline-flex items-center gap-1.5 pt-2 text-sm font-medium text-tn-red">
                  <XCircle className="size-4" /> {metadataTestResult.detail}
                </p>
              )
            ) : null}
          </div>
        )}
      </SettingsCard>

      {/* Renaming */}
      <SettingsCard
        title="Renaming"
        description="How imported files and folders are named."
        actions={
          !editingRename && !renameConfig.isError ? (
            <Button
              size="sm"
              aria-label="Edit renaming settings"
              onClick={() => setEditingRename(true)}
              className="gap-1.5"
            >
              <Pencil className="size-3.5" />
              Edit
            </Button>
          ) : undefined
        }
      >
        {renameConfig.isError ? (
          <p className="text-sm text-tn-red">Couldn't load the renaming settings.</p>
        ) : editingRename ? (
          <RenameSettingsForm
            initial={{
              folderFormat: renameCurrent?.folderFormat ?? "",
              fileFormat: renameCurrent?.fileFormat ?? "",
              renameEnabled: renameCurrent?.renameEnabled ?? true,
              issuePaddingEnabled: renameCurrent?.issuePaddingEnabled ?? true,
              paddingFormat: renameCurrent?.paddingFormat || "00x",
              replaceSpaces: renameCurrent?.replaceSpaces ?? false,
              lowercase: renameCurrent?.lowercase ?? false,
            }}
            pending={renameUpdate.isPending}
            error={renameUpdate.isError ? RENAME_SAVE_ERROR : undefined}
            onSubmit={submitRenameEdit}
            onCancel={() => setEditingRename(false)}
          />
        ) : (
          <div className="rounded-lg border border-border bg-tn-night/30 p-4">
            <ReadRow label="Folder Format">{renameCurrent?.folderFormat || "—"}</ReadRow>
            <ReadRow label="File Format">{renameCurrent?.fileFormat || "—"}</ReadRow>
            <ReadRow label="Rename files">{renameCurrent?.renameEnabled ? "On" : "Off"}</ReadRow>
            <ReadRow label="Issue Number Padding">
              {renameCurrent?.issuePaddingEnabled
                ? `On (${renameCurrent?.paddingFormat || "00x"})`
                : "Off"}
            </ReadRow>
            <ReadRow label="Replace Spaces">{renameCurrent?.replaceSpaces ? "On" : "Off"}</ReadRow>
            <ReadRow label="Lowercase">{renameCurrent?.lowercase ? "On" : "Off"}</ReadRow>
          </div>
        )}
      </SettingsCard>

      {/* Direct Downloads */}
      <SettingsCard
        title="Direct Downloads"
        description="GetComics fallback when no indexer has an acceptable release."
        actions={
          !editingDDL && !ddlConfig.isError ? (
            <Button
              size="sm"
              aria-label="Edit DDL settings"
              onClick={() => setEditingDDL(true)}
              className="gap-1.5"
            >
              <Pencil className="size-3.5" />
              Edit
            </Button>
          ) : undefined
        }
      >
        {ddlConfig.isError ? (
          <p className="text-sm text-tn-red">Couldn't load the DDL settings.</p>
        ) : editingDDL ? (
          <DDLSettingsForm
            initial={{ enabled: ddlCurrent?.enabled ?? false }}
            pending={ddlUpdate.isPending}
            error={ddlUpdate.isError ? DDL_SAVE_ERROR : undefined}
            onSubmit={submitDDLEdit}
            onCancel={() => setEditingDDL(false)}
          />
        ) : (
          <div className="flex flex-col gap-2 rounded-lg border border-border bg-tn-night/30 p-4 text-sm">
            <span className="text-muted-foreground">
              Enable DDLs:{" "}
              <span className="font-mono text-foreground">
                {ddlCurrent?.enabled ? "On" : "Off"}
              </span>
            </span>
            <span className="text-muted-foreground">
              GetComics is used as a fallback when no Newznab indexer has an acceptable
              release.
            </span>
          </div>
        )}
      </SettingsCard>

      {/* Authentication */}
      <SettingsCard
        title="Authentication"
        description="Optional single-user sign-in for omnibus."
        actions={
          !editingAuth && !authConfig.isError ? (
            <Button
              size="sm"
              aria-label="Edit authentication settings"
              onClick={() => setEditingAuth(true)}
              className="gap-1.5"
            >
              <Pencil className="size-3.5" />
              Edit
            </Button>
          ) : undefined
        }
      >
        {authConfig.isError ? (
          <p className="text-sm text-tn-red">Couldn't load the authentication settings.</p>
        ) : editingAuth ? (
          <AuthSettingsForm
            initial={{
              mode: authCurrent?.mode ?? AuthMode.OFF,
              username: authCurrent?.username ?? "",
            }}
            pending={authUpdate.isPending}
            error={authUpdate.isError ? AUTH_SAVE_ERROR : undefined}
            onSubmit={submitAuthEdit}
            onCancel={() => setEditingAuth(false)}
          />
        ) : (
          <div className="rounded-lg border border-border bg-tn-night/30 p-4">
            <div className="flex items-center gap-2.5 pb-2">
              <span className="text-sm font-medium text-foreground">
                {authModeLabel(authCurrent?.mode)}
              </span>
              <Badge variant={authCurrent?.configured ? "success" : "neutral"} dot>
                {authCurrent?.configured ? "Configured" : "Not configured"}
              </Badge>
            </div>
            <ReadRow label="Mode">{authModeLabel(authCurrent?.mode)}</ReadRow>
            <ReadRow label="Username">{authCurrent?.username || "—"}</ReadRow>
            {!authCurrent?.configured ? (
              <p className="pt-2 text-sm text-muted-foreground">
                Authentication is off. Set a username and password and switch it on to
                require sign-in.
              </p>
            ) : null}
          </div>
        )}
      </SettingsCard>
    </div>
  );
}
