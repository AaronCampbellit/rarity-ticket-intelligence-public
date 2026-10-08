import { CustomDateEditor } from "../calendar/CustomDateEditor";
import {
  type FormEvent,
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";

import { csrfHeaders } from "../../api/browserSession";
import { clientContextHeaders } from "../../api/clientContext";
import {
  Button,
  ButtonGroup,
  DataTable,
  Dialog,
  Notice,
  Page,
  SearchInput,
  StatePanel,
  StatusBadge,
} from "../../design-system";
import "../operations/operations.css";
import { DeferredObjectTagEditor } from "../classification/ObjectTagEditor";
import { ObjectTagSummary } from "../classification/ObjectTagSummary";
import { RequiredClassificationPicker } from "../classification/RequiredClassificationPicker";
import {
  createClassificationAPI,
  isClassificationCreateError,
} from "../classification/api";
import "./organization.css";

type ResourceKind = "location" | "contact" | "asset" | "service" | "contract";
type LifecycleFilter = "active" | "inactive" | "all";

type Summary = {
  id: string;
  kind: ResourceKind;
  display_id: string;
  name: string;
  detail?: string;
  location_id?: string;
  lifecycle_state: "active" | "inactive";
  authority?: "technician_confirmed" | "discovered";
  version: number;
};

type Detail = Summary & {
  email?: string;
  phone?: string;
  asset_type?: string;
  criticality?: string;
  starts_on?: string;
  ends_on?: string;
};

type LifecycleAction = {
  item: Summary;
  operation: "deactivate" | "reactivate";
};

type CatalogSnapshot = {
  clientID: string;
  filter: LifecycleFilter;
  items: Summary[];
};

type LocationSnapshot = {
  clientID: string;
  items: Summary[];
};

const kinds: ResourceKind[] = [
  "location",
  "contact",
  "asset",
  "service",
  "contract",
];

export function ClientResourcesPage({
  clientID,
  capabilities,
  selectedAssetID,
  refreshToken = 0,
}: {
  clientID: string;
  capabilities: Set<string>;
  selectedAssetID?: string;
  refreshToken?: number;
}) {
  const [catalog, setCatalog] = useState<CatalogSnapshot>();
  const [locationCatalog, setLocationCatalog] = useState<LocationSnapshot>();
  const [filter, setFilter] = useState<LifecycleFilter>("active");
  const [state, setState] = useState<"loading" | "ready" | "saving" | "error">(
    "loading",
  );
  const [message, setMessage] = useState("");
  const [error, setError] = useState("");
  const [activeKind, setActiveKind] = useState<"all" | ResourceKind>("all");
  const [query, setQuery] = useState("");
  const [createOpen, setCreateOpen] = useState(false);
  const [createKind, setCreateKind] = useState<ResourceKind>("location");
  const [editing, setEditing] = useState<Detail>();
  const [lifecycleAction, setLifecycleAction] = useState<LifecycleAction>();
  const [assetTagIDs, setAssetTagIDs] = useState<string[]>([]);
  const [assetClassificationError, setAssetClassificationError] = useState("");
  const [selectedAsset, setSelectedAsset] = useState<Summary>();
  const assetClassificationRef = useRef<HTMLInputElement>(null);
  const catalogRequest = useRef(0);
  const detailRequest = useRef(0);
  const locationRequest = useRef(0);
  const clientRef = useRef(clientID);
  const filterRef = useRef(filter);
  clientRef.current = clientID;
  filterRef.current = filter;

  const canFilterLifecycle = kinds.some((kind) =>
    capabilities.has(`${kind}.lifecycle`),
  );
  const createKinds = kinds.filter((kind) =>
    capabilities.has(`${kind}.create`),
  );
  const needsActiveLocations = [
    "contact.create",
    "contact.update",
    "asset.create",
    "asset.update",
  ].some((capability) => capabilities.has(capability));
  const catalogMatchesContext =
    catalog?.clientID === clientID && catalog.filter === filter;
  const items = catalogMatchesContext ? catalog.items : [];
  const activeLocations =
    locationCatalog?.clientID === clientID ? locationCatalog.items : [];
  const normalizedQuery = query.trim().toLowerCase();
  const visibleItems = items.filter(
    (item) =>
      (activeKind === "all" || item.kind === activeKind) &&
      (!normalizedQuery ||
        `${item.display_id} ${item.name} ${item.detail ?? ""}`
          .toLowerCase()
          .includes(normalizedQuery)),
  );

  useEffect(() => {
    if (!selectedAssetID) return;
    if (selectedAsset?.id === selectedAssetID) return;
    const listed = items.find(
      (item) => item.kind === "asset" && item.id === selectedAssetID,
    );
    if (listed) {
      setSelectedAsset(listed);
      return;
    }
    const controller = new AbortController();
    void fetch(
      `/api/v1/client-resources/${encodeURIComponent(selectedAssetID)}`,
      {
        credentials: "same-origin",
        headers: clientContextHeaders(clientID),
        signal: controller.signal,
      },
    )
      .then(async (response) => {
        if (!response.ok) throw new Error("Client resource unavailable");
        const exact = (await response.json()) as Summary;
        if (!controller.signal.aborted && exact.kind === "asset")
          setSelectedAsset(exact);
      })
      .catch(() => {
        if (!controller.signal.aborted) setState("error");
      });
    return () => controller.abort();
  }, [clientID, items, selectedAsset?.id, selectedAssetID]);

  useLayoutEffect(() => {
    catalogRequest.current += 1;
    detailRequest.current += 1;
    setCatalog(undefined);
    setEditing(undefined);
    setLifecycleAction(undefined);
    setCreateOpen(false);
    setAssetTagIDs([]);
    setAssetClassificationError("");
    setSelectedAsset(undefined);
    setMessage("");
    setError("");
    setState(clientID ? "loading" : "error");
  }, [clientID, filter]);

  useLayoutEffect(() => {
    locationRequest.current += 1;
    setLocationCatalog(undefined);
  }, [clientID, refreshToken]);

  const loadCatalog = useCallback(async (signal?: AbortSignal) => {
    const targetClient = clientRef.current;
    const targetFilter = filterRef.current;
    const requestID = ++catalogRequest.current;
    if (!targetClient) {
      setCatalog(undefined);
      setState("error");
      return;
    }
    setState("loading");
    try {
      const response = await fetch(
        `/api/v1/client-resources?limit=500&lifecycle=${targetFilter}`,
        {
          credentials: "same-origin",
          headers: clientContextHeaders(targetClient),
          signal,
        },
      );
      if (!response.ok) throw new Error("Client resources unavailable");
      const next = (await response.json()) as Summary[];
      if (
        signal?.aborted ||
        requestID !== catalogRequest.current ||
        targetClient !== clientRef.current ||
        targetFilter !== filterRef.current
      ) {
        return;
      }
      setCatalog({
        clientID: targetClient,
        filter: targetFilter,
        items: next,
      });
      setState("ready");
    } catch {
      if (!signal?.aborted && requestID === catalogRequest.current) {
        setCatalog(undefined);
        setState("error");
      }
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void loadCatalog(controller.signal);
    return () => controller.abort();
  }, [clientID, filter, loadCatalog, refreshToken]);

  const loadActiveLocations = useCallback(
    async (signal?: AbortSignal) => {
      const targetClient = clientRef.current;
      const requestID = ++locationRequest.current;
      if (!targetClient || !needsActiveLocations) {
        setLocationCatalog(undefined);
        return;
      }
      try {
        const response = await fetch(
          "/api/v1/client-resources?limit=500&lifecycle=active",
          {
            credentials: "same-origin",
            headers: clientContextHeaders(targetClient),
            signal,
          },
        );
        if (!response.ok) throw new Error("Active Locations unavailable");
        const next = ((await response.json()) as Summary[]).filter(
          (item) =>
            item.kind === "location" && item.lifecycle_state === "active",
        );
        if (
          signal?.aborted ||
          requestID !== locationRequest.current ||
          targetClient !== clientRef.current
        ) {
          return;
        }
        setLocationCatalog({ clientID: targetClient, items: next });
      } catch {
        if (!signal?.aborted && requestID === locationRequest.current)
          setLocationCatalog(undefined);
      }
    },
    [needsActiveLocations],
  );

  useEffect(() => {
    const controller = new AbortController();
    void loadActiveLocations(controller.signal);
    return () => controller.abort();
  }, [clientID, loadActiveLocations, refreshToken]);

  async function refreshResourceData() {
    locationRequest.current += 1;
    setLocationCatalog(undefined);
    await Promise.all([loadCatalog(), loadActiveLocations()]);
  }

  async function create(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (createKind === "asset" && !assetTagIDs.length) {
      setAssetClassificationError(
        "Select at least one meaningful classification tag.",
      );
      assetClassificationRef.current?.focus();
      return;
    }
    const form = event.currentTarget;
    const data = new FormData(form);
    const displayID = String(data.get("display_id") ?? "").trim();
    const payload = createPayload(createKind, data);
    if (createKind === "asset") payload.tag_ids = assetTagIDs;
    const mutationClient = clientRef.current;
    setState("saving");
    setMessage("");
    setError("");
    try {
      const response = await fetch(`/api/v1/${plural(createKind)}`, {
        method: "POST",
        credentials: "same-origin",
        headers: mutationHeaders(mutationClient),
        body: JSON.stringify(payload),
      });
      if (!response.ok) throw await mutationFailure(response);
      if (mutationClient !== clientRef.current) return;
      const refresh = refreshResourceData();
      setCreateOpen(false);
      setAssetTagIDs([]);
      setAssetClassificationError("");
      form.reset();
      setMessage(`${label(createKind)} ${displayID} created.`);
      await refresh;
    } catch (caught) {
      if (mutationClient !== clientRef.current) return;
      setState("ready");
      if (createKind === "asset" && isClassificationCreateError(caught)) {
        setAssetClassificationError(
          "Classification changed. Confirm at least one current classification tag.",
        );
        assetClassificationRef.current?.focus();
      } else {
        setError(safeMutationMessage(caught));
      }
    }
  }

  async function openEditor(item: Summary) {
    const mutationClient = clientRef.current;
    const requestID = ++detailRequest.current;
    setError("");
    try {
      const response = await fetch(`/api/v1/${plural(item.kind)}/${item.id}`, {
        credentials: "same-origin",
        headers: clientContextHeaders(mutationClient),
      });
      if (!response.ok) throw await mutationFailure(response);
      const detail = (await response.json()) as Detail;
      if (
        requestID !== detailRequest.current ||
        mutationClient !== clientRef.current
      )
        return;
      setEditing(detail);
    } catch (caught) {
      if (
        requestID === detailRequest.current &&
        mutationClient === clientRef.current
      )
        setError(safeMutationMessage(caught));
    }
  }

  async function updateResource(
    detail: Detail,
    patch: Record<string, unknown>,
    reason: string,
  ) {
    const mutationClient = clientRef.current;
    setState("saving");
    setError("");
    try {
      const response = await fetch(
        `/api/v1/${plural(detail.kind)}/${detail.id}`,
        {
          method: "PATCH",
          credentials: "same-origin",
          headers: mutationHeaders(mutationClient, detail.version),
          body: JSON.stringify({
            expected_version: detail.version,
            reason,
            ...patch,
          }),
        },
      );
      if (!response.ok) throw await mutationFailure(response);
      if (mutationClient !== clientRef.current) return;
      const refresh = refreshResourceData();
      setEditing(undefined);
      setMessage(`${label(detail.kind)} ${detail.display_id} updated.`);
      await refresh;
    } catch (caught) {
      if (mutationClient !== clientRef.current) return;
      setState("ready");
      setError(safeMutationMessage(caught));
    }
  }

  async function changeLifecycle(action: LifecycleAction, reason: string) {
    const mutationClient = clientRef.current;
    setState("saving");
    setError("");
    try {
      const response = await fetch(
        `/api/v1/${plural(action.item.kind)}/${action.item.id}/${action.operation}`,
        {
          method: "POST",
          credentials: "same-origin",
          headers: mutationHeaders(mutationClient, action.item.version),
          body: JSON.stringify({
            expected_version: action.item.version,
            reason,
          }),
        },
      );
      if (!response.ok) throw await mutationFailure(response);
      if (mutationClient !== clientRef.current) return;
      const refresh = refreshResourceData();
      setLifecycleAction(undefined);
      setMessage(
        `${label(action.item.kind)} ${action.item.display_id} ${
          action.operation === "deactivate" ? "deactivated" : "reactivated"
        }.`,
      );
      await refresh;
    } catch (caught) {
      if (mutationClient !== clientRef.current) return;
      setState("ready");
      setError(safeMutationMessage(caught));
    }
  }

  return (
    <Page
      className="operations-health client-resources"
      eyebrow="Active client"
      title="Client resources"
      description="Maintain contacts, locations, assets, services, and effective contracts for service delivery."
    >
      <section
        className="client-resources__toolbar"
        aria-label="Resource catalog controls"
      >
        <label>
          Lifecycle
          <select
            value={filter}
            onChange={(event) =>
              setFilter(event.target.value as LifecycleFilter)
            }
          >
            <option value="active">Active</option>
            {canFilterLifecycle ? (
              <option value="inactive">Inactive</option>
            ) : null}
            {canFilterLifecycle ? <option value="all">All</option> : null}
          </select>
        </label>
        {createKinds.length ? (
          <Button
            intent="primary"
            size="compact"
            onClick={() => {
              setCreateKind(createKinds[0]);
              setCreateOpen(true);
            }}
          >
            Add resource
          </Button>
        ) : null}
      </section>

      {state === "loading" && !items.length ? (
        <StatePanel state="loading" title="Loading client resources…" />
      ) : null}
      {state === "error" ? (
        <StatePanel
          state="error"
          title="Client resources are unavailable"
          description="Verify the active client and your permissions."
        />
      ) : null}
      {message ? (
        <Notice tone="success" title="Client resources updated">
          {message}
        </Notice>
      ) : null}
      {error ? (
        <div role="alert" aria-label="Client resource change blocked">
          <Notice tone="danger" title="Client resource change blocked">
            {error}
          </Notice>
        </div>
      ) : null}

      <section aria-labelledby="resource-catalog-heading">
        <div className="resource-catalog__header">
          <div>
            <h2 id="resource-catalog-heading">Resource catalog</h2>
            <p>{visibleItems.length} visible records for the active client.</p>
          </div>
          <SearchInput
            aria-label="Search client resources"
            placeholder="Search resources…"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
          {state === "saving" ? <span role="status">Saving…</span> : null}
        </div>
        <div className="resource-kind-switcher" aria-label="Resource type">
          {(["all", ...kinds] as const).map((kind) => (
            <button
              type="button"
              key={kind}
              aria-pressed={activeKind === kind}
              onClick={() => setActiveKind(kind)}
            >
              {kind === "all" ? "All resources" : `${label(kind)}s`}
              <span>
                {kind === "all"
                  ? items.length
                  : items.filter((item) => item.kind === kind).length}
              </span>
            </button>
          ))}
        </div>
        {!visibleItems.length && state === "ready" ? (
          <p>No matching client resources.</p>
        ) : null}
        {visibleItems.length ? (
          <DataTable
            caption="Client resource lifecycle catalog"
            rows={visibleItems}
            getRowID={(item) => item.id}
            columns={[
              { id: "kind", header: "Kind", cell: (item) => label(item.kind) },
              {
                id: "display",
                header: "Display ID",
                cell: (item) => <strong>{item.display_id}</strong>,
              },
              {
                id: "name",
                header: "Name",
                cell: (item) =>
                  item.kind === "asset" ? (
                    <button
                      type="button"
                      onClick={() => setSelectedAsset(item)}
                    >
                      {item.name}
                    </button>
                  ) : (
                    item.name
                  ),
              },
              {
                id: "detail",
                header: "Detail",
                cell: (item) => item.detail || "—",
              },
              {
                id: "classification",
                header: "Classification",
                cell: (item) =>
                  item.kind === "asset" ? (
                    <ObjectTagSummary
                      clientID={clientID}
                      target={{ objectType: "asset", objectId: item.id }}
                    />
                  ) : (
                    "—"
                  ),
              },
              {
                id: "status",
                header: "Status",
                cell: (item) => (
                  <StatusBadge
                    tone={
                      item.lifecycle_state === "active" ? "success" : "neutral"
                    }
                  >
                    {item.lifecycle_state === "active" ? "Active" : "Inactive"}
                  </StatusBadge>
                ),
              },
              {
                id: "version",
                header: "Version",
                cell: (item) => `v${item.version}`,
              },
              {
                id: "actions",
                header: "Actions",
                align: "end",
                cell: (item) => (
                  <ResourceActions
                    item={item}
                    capabilities={capabilities}
                    onEdit={() => void openEditor(item)}
                    onLifecycle={(operation) =>
                      setLifecycleAction({ item, operation })
                    }
                  />
                ),
              },
            ]}
          />
        ) : null}
      </section>

      {selectedAsset ? (
        <section
          className="settings-card"
          aria-labelledby="asset-classification-heading"
        >
          <h2 id="asset-classification-heading">{selectedAsset.name}</h2>
          <DeferredObjectTagEditor
            api={createClassificationAPI(globalThis.fetch, clientID)}
            clientID={clientID}
            target={{ objectType: "asset", objectId: selectedAsset.id }}
          />
          <CustomDateEditor
            objectType="asset"
            objectID={selectedAsset.id}
            clientID={clientID}
          />
        </section>
      ) : null}

      <Dialog
        open={createOpen}
        title="Add client resource"
        description="Create one resource for the active Client."
        onClose={() => setCreateOpen(false)}
        variant="drawer"
      >
        <form className="client-resource-form" onSubmit={create}>
          <label>
            Resource kind
            <select
              value={createKind}
              onChange={(event) => {
                setCreateKind(event.target.value as ResourceKind);
                setAssetTagIDs([]);
                setAssetClassificationError("");
              }}
            >
              {createKinds.map((kind) => (
                <option key={kind} value={kind}>
                  {label(kind)}
                </option>
              ))}
            </select>
          </label>
          <CreateFields kind={createKind} locations={activeLocations} />
          {createKind === "asset" ? (
            <RequiredClassificationPicker
              clientID={clientID}
              selectedIDs={assetTagIDs}
              error={assetClassificationError}
              inputRef={assetClassificationRef}
              onChange={(ids) => {
                setAssetTagIDs(ids);
                setAssetClassificationError("");
              }}
            />
          ) : null}
          <ButtonGroup label="Create resource actions">
            <Button
              type="submit"
              intent="primary"
              disabled={createKind === "asset" && !assetTagIDs.length}
            >
              Create {label(createKind)}
            </Button>
            <Button onClick={() => setCreateOpen(false)}>Cancel</Button>
          </ButtonGroup>
        </form>
      </Dialog>

      {editing ? (
        <EditResourceDialog
          key={`${editing.id}:${editing.version}`}
          detail={editing}
          locations={activeLocations}
          onClose={() => setEditing(undefined)}
          onSave={(patch, reason) =>
            void updateResource(editing, patch, reason)
          }
        />
      ) : null}
      {lifecycleAction ? (
        <LifecycleDialog
          key={`${lifecycleAction.item.id}:${lifecycleAction.operation}`}
          action={lifecycleAction}
          onClose={() => setLifecycleAction(undefined)}
          onConfirm={(reason) => void changeLifecycle(lifecycleAction, reason)}
        />
      ) : null}
    </Page>
  );
}

function ResourceActions({
  item,
  capabilities,
  onEdit,
  onLifecycle,
}: {
  item: Summary;
  capabilities: Set<string>;
  onEdit: () => void;
  onLifecycle: (operation: "deactivate" | "reactivate") => void;
}) {
  if (item.kind === "asset" && item.authority === "discovered") {
    return (
      <span className="client-resources__locked">Discovered · locked</span>
    );
  }
  const canEdit =
    capabilities.has(`${item.kind}.update`) &&
    item.lifecycle_state === "active";
  const canLifecycle = capabilities.has(`${item.kind}.lifecycle`);
  if (!canEdit && !canLifecycle)
    return <span aria-label="No available actions">—</span>;
  return (
    <div className="client-resources__row-actions">
      {canEdit ? (
        <Button
          size="compact"
          onClick={onEdit}
          aria-label={`Edit ${item.display_id}`}
        >
          Edit
        </Button>
      ) : null}
      {canLifecycle ? (
        <Button
          size="compact"
          intent={item.lifecycle_state === "active" ? "danger" : "secondary"}
          aria-label={`${item.lifecycle_state === "active" ? "Deactivate" : "Reactivate"} ${item.display_id}`}
          onClick={() =>
            onLifecycle(
              item.lifecycle_state === "active" ? "deactivate" : "reactivate",
            )
          }
        >
          {item.lifecycle_state === "active" ? "Deactivate" : "Reactivate"}
        </Button>
      ) : null}
    </div>
  );
}

function EditResourceDialog({
  detail,
  locations,
  onClose,
  onSave,
}: {
  detail: Detail;
  locations: Summary[];
  onClose: () => void;
  onSave: (patch: Record<string, unknown>, reason: string) => void;
}) {
  const [values, setValues] = useState(() => detailValues(detail));
  const [touched, setTouched] = useState<Set<string>>(() => new Set());
  const [reason, setReason] = useState("");
  const [validation, setValidation] = useState("");

  function change(field: string, value: string) {
    setValues((current) => ({ ...current, [field]: value }));
    setTouched((current) => new Set(current).add(field));
  }

  function clear(field: string, checked: boolean) {
    if (checked) change(field, "");
    else
      setTouched((current) => {
        const next = new Set(current);
        next.delete(field);
        return next;
      });
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!reason.trim()) {
      setValidation("Enter a change reason.");
      return;
    }
    const patch = editPatch(detail.kind, values, touched);
    if (!Object.keys(patch).length) {
      setValidation(
        "Change at least one field or use an explicit clear control.",
      );
      return;
    }
    onSave(patch, reason.trim());
  }

  return (
    <Dialog
      open
      title={`Edit ${label(detail.kind)} ${detail.display_id}`}
      description="Only approved business fields can be changed."
      onClose={onClose}
      variant="drawer"
    >
      <form className="client-resource-form" onSubmit={submit} noValidate>
        <label>
          Current version
          <input value={String(detail.version)} readOnly />
        </label>
        <EditFields
          kind={detail.kind}
          values={values}
          touched={touched}
          locations={locations}
          onChange={change}
          onClear={clear}
        />
        <label>
          Change reason
          <textarea
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            required
          />
        </label>
        {validation ? (
          <p className="client-resource-form__error">{validation}</p>
        ) : null}
        <ButtonGroup label="Edit resource actions">
          <Button type="submit" intent="primary">
            Save changes
          </Button>
          <Button onClick={onClose}>Cancel</Button>
        </ButtonGroup>
      </form>
    </Dialog>
  );
}

function LifecycleDialog({
  action,
  onClose,
  onConfirm,
}: {
  action: LifecycleAction;
  onClose: () => void;
  onConfirm: (reason: string) => void;
}) {
  const [reason, setReason] = useState("");
  const [validation, setValidation] = useState("");
  const verb =
    action.operation === "deactivate" ? "deactivation" : "reactivation";
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!reason.trim()) {
      setValidation("Enter a reason.");
      return;
    }
    onConfirm(reason.trim());
  }
  return (
    <Dialog
      open
      title={`${action.operation === "deactivate" ? "Deactivate" : "Reactivate"} ${label(action.item.kind)} ${action.item.display_id}`}
      description={`Version ${action.item.version} will be required at confirmation.`}
      onClose={onClose}
    >
      <form className="client-resource-form" onSubmit={submit} noValidate>
        <label>
          Reason
          <textarea
            value={reason}
            onChange={(event) => setReason(event.target.value)}
            required
          />
        </label>
        {validation ? (
          <p className="client-resource-form__error">{validation}</p>
        ) : null}
        <ButtonGroup label={`${verb} confirmation`}>
          <Button
            type="submit"
            intent={action.operation === "deactivate" ? "danger" : "primary"}
          >
            Confirm {verb}
          </Button>
          <Button onClick={onClose}>Cancel</Button>
        </ButtonGroup>
      </form>
    </Dialog>
  );
}

function CreateFields({
  kind,
  locations,
}: {
  kind: ResourceKind;
  locations: Summary[];
}) {
  return (
    <>
      <div className="client-resource-form__grid">
        <label>
          Display ID
          <input name="display_id" required />
        </label>
        <label>
          {kind === "contact" ? "Display name" : "Name"}
          <input name="name" required />
        </label>
      </div>
      {kind === "contact" ? (
        <>
          <div className="client-resource-form__grid">
            <label>
              Email
              <input name="email" type="email" />
            </label>
            <label>
              Phone
              <input name="phone" />
            </label>
          </div>
          <LocationSelect locations={locations} />
        </>
      ) : null}
      {kind === "asset" ? (
        <>
          <label>
            Asset type
            <input name="asset_type" required />
          </label>
          <LocationSelect locations={locations} />
          <div className="client-resource-form__grid">
            <label>
              Source system
              <input name="source_system" defaultValue="manual" />
            </label>
            <label>
              External ID
              <input name="external_id" />
            </label>
          </div>
        </>
      ) : null}
      {kind === "service" ? (
        <label>
          Criticality
          <select name="criticality" defaultValue="normal">
            {criticalityOptions()}
          </select>
        </label>
      ) : null}
      {kind === "contract" ? (
        <div className="client-resource-form__grid">
          <label>
            Starts on
            <input name="starts_on" type="date" required />
          </label>
          <label>
            Ends on
            <input name="ends_on" type="date" />
          </label>
        </div>
      ) : null}
    </>
  );
}

function EditFields({
  kind,
  values,
  touched,
  locations,
  onChange,
  onClear,
}: {
  kind: ResourceKind;
  values: Record<string, string>;
  touched: Set<string>;
  locations: Summary[];
  onChange: (field: string, value: string) => void;
  onClear: (field: string, checked: boolean) => void;
}) {
  const nameField = kind === "contact" ? "display_name" : "name";
  return (
    <>
      <label>
        {kind === "contact" ? "Display name" : "Name"}
        <input
          value={values[nameField] ?? ""}
          onChange={(event) => onChange(nameField, event.target.value)}
        />
      </label>
      {kind === "contact" ? (
        <>
          <OptionalField
            label="Email"
            field="email"
            type="email"
            values={values}
            touched={touched}
            onChange={onChange}
            onClear={onClear}
          />
          <OptionalField
            label="Phone"
            field="phone"
            values={values}
            touched={touched}
            onChange={onChange}
            onClear={onClear}
          />
          <LocationEdit
            values={values}
            touched={touched}
            locations={locations}
            onChange={onChange}
            onClear={onClear}
          />
        </>
      ) : null}
      {kind === "asset" ? (
        <>
          <label>
            Asset type
            <input
              value={values.asset_type ?? ""}
              onChange={(event) => onChange("asset_type", event.target.value)}
            />
          </label>
          <LocationEdit
            values={values}
            touched={touched}
            locations={locations}
            onChange={onChange}
            onClear={onClear}
          />
        </>
      ) : null}
      {kind === "service" ? (
        <label>
          Criticality
          <select
            value={values.criticality ?? ""}
            onChange={(event) => onChange("criticality", event.target.value)}
          >
            <option value="">Not set</option>
            {criticalityOptions()}
          </select>
        </label>
      ) : null}
      {kind === "contract" ? (
        <>
          <label>
            Starts on
            <input
              type="date"
              value={values.starts_on ?? ""}
              onChange={(event) => onChange("starts_on", event.target.value)}
            />
          </label>
          <OptionalField
            label="Ends on"
            field="ends_on"
            type="date"
            values={values}
            touched={touched}
            onChange={onChange}
            onClear={onClear}
          />
        </>
      ) : null}
    </>
  );
}

function OptionalField({
  label: fieldLabel,
  field,
  type,
  values,
  touched,
  onChange,
  onClear,
}: {
  label: string;
  field: string;
  type?: string;
  values: Record<string, string>;
  touched: Set<string>;
  onChange: (field: string, value: string) => void;
  onClear: (field: string, checked: boolean) => void;
}) {
  const cleared = touched.has(field) && values[field] === "";
  return (
    <fieldset className="client-resource-form__optional">
      <label>
        {fieldLabel}
        <input
          type={type}
          value={values[field] ?? ""}
          disabled={cleared}
          onChange={(event) => onChange(field, event.target.value)}
        />
      </label>
      <label className="client-resource-form__clear">
        <input
          type="checkbox"
          checked={cleared}
          onChange={(event) => onClear(field, event.target.checked)}
        />
        Clear {fieldLabel.toLowerCase()}
      </label>
    </fieldset>
  );
}

function LocationEdit({
  values,
  touched,
  locations,
  onChange,
  onClear,
}: {
  values: Record<string, string>;
  touched: Set<string>;
  locations: Summary[];
  onChange: (field: string, value: string) => void;
  onClear: (field: string, checked: boolean) => void;
}) {
  const cleared = touched.has("location_id") && values.location_id === "";
  return (
    <fieldset className="client-resource-form__optional">
      <label>
        Location
        <select
          value={values.location_id ?? ""}
          disabled={cleared}
          onChange={(event) => onChange("location_id", event.target.value)}
        >
          <option value="">No location</option>
          {locations.map((location) => (
            <option key={location.id} value={location.id}>
              {location.name} ({location.display_id})
            </option>
          ))}
        </select>
      </label>
      <label className="client-resource-form__clear">
        <input
          type="checkbox"
          checked={cleared}
          onChange={(event) => onClear("location_id", event.target.checked)}
        />
        Clear location
      </label>
    </fieldset>
  );
}

function LocationSelect({ locations }: { locations: Summary[] }) {
  return (
    <label>
      Location
      <select name="location_id">
        <option value="">No location</option>
        {locations.map((item) => (
          <option key={item.id} value={item.id}>
            {item.name} ({item.display_id})
          </option>
        ))}
      </select>
    </label>
  );
}

function detailValues(detail: Detail): Record<string, string> {
  return {
    name: detail.name,
    display_name: detail.name,
    email: detail.email ?? "",
    phone: detail.phone ?? "",
    location_id: detail.location_id ?? "",
    asset_type: detail.asset_type ?? detail.detail ?? "",
    criticality: detail.criticality ?? detail.detail ?? "",
    starts_on: dateInput(detail.starts_on),
    ends_on: dateInput(detail.ends_on),
  };
}

function editPatch(
  kind: ResourceKind,
  values: Record<string, string>,
  touched: Set<string>,
) {
  const allowed: Record<ResourceKind, string[]> = {
    location: ["name"],
    contact: ["display_name", "email", "phone", "location_id"],
    asset: ["name", "asset_type", "location_id"],
    service: ["name", "criticality"],
    contract: ["name", "starts_on", "ends_on"],
  };
  const patch: Record<string, unknown> = {};
  for (const field of allowed[kind]) {
    if (!touched.has(field)) continue;
    if (field === "starts_on" || (field === "ends_on" && values[field])) {
      patch[field] = dateValue(values[field]);
    } else if (field === "ends_on") {
      patch.clear_ends_on = true;
    } else {
      patch[field] = values[field];
    }
  }
  return patch;
}

function createPayload(
  kind: ResourceKind,
  data: FormData,
): Record<string, unknown> {
  const common = { display_id: data.get("display_id"), name: data.get("name") };
  switch (kind) {
    case "location":
      return common;
    case "contact":
      return {
        display_id: data.get("display_id"),
        display_name: data.get("name"),
        email: data.get("email"),
        phone: data.get("phone"),
        location_id: data.get("location_id"),
      };
    case "asset":
      return {
        ...common,
        asset_type: data.get("asset_type"),
        location_id: data.get("location_id"),
        source_system: data.get("source_system"),
        external_id: data.get("external_id"),
        authority: "technician_confirmed",
      };
    case "service":
      return { ...common, criticality: data.get("criticality") };
    case "contract":
      return {
        ...common,
        starts_on: dateValue(data.get("starts_on")),
        ...(data.get("ends_on")
          ? { ends_on: dateValue(data.get("ends_on")) }
          : {}),
      };
  }
}

function mutationHeaders(clientID: string, version?: number) {
  return {
    "Content-Type": "application/json",
    ...csrfHeaders(),
    ...clientContextHeaders(clientID),
    ...(version === undefined ? {} : { "If-Match": `"${version}"` }),
  };
}

async function mutationFailure(response: Response) {
  let code = "unknown";
  try {
    const body = (await response.json()) as { error?: { code?: string } };
    code = body.error?.code ?? code;
  } catch {
    // The safe fallback below intentionally ignores untrusted response text.
  }
  return new Error(code);
}

function safeMutationMessage(caught: unknown) {
  const code = caught instanceof Error ? caught.message : "unknown";
  switch (code) {
    case "version_conflict":
      return "This resource changed. Reload the current version and try again.";
    case "lifecycle_conflict":
      return "The resource is already in another lifecycle state. Reload and try again.";
    case "resource_in_use":
      return "This resource is in use. Resolve its active dependencies before deactivating it.";
    case "resource_authority_conflict":
      return "Discovered Assets are managed by their source system and are locked here.";
    case "forbidden":
      return "Your authority changed. Reload before trying this action again.";
    case "validation_failed":
      return "Review the required fields, exact version, and reason.";
    default:
      return "The resource could not be changed. Reload and try again.";
  }
}

function plural(kind: ResourceKind) {
  return kind === "contract" ? "contracts" : `${kind}s`;
}
function label(kind: ResourceKind) {
  return kind[0].toUpperCase() + kind.slice(1);
}
function dateInput(value?: string) {
  return value ? value.slice(0, 10) : "";
}
function dateValue(value: FormDataEntryValue | string | null) {
  return new Date(`${String(value)}T00:00:00.000Z`).toISOString();
}
function criticalityOptions() {
  return (
    <>
      <option value="low">Low</option>
      <option value="normal">Normal</option>
      <option value="high">High</option>
      <option value="critical">Critical</option>
    </>
  );
}
