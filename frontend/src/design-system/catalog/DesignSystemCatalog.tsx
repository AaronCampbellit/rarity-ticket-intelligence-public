import { ClipboardList, PanelRight } from "lucide-react";
import { useState } from "react";

import { useMainContentID } from "../foundations/mainContent";
import { Button } from "../components/actions/Button";
import { ButtonGroup } from "../components/actions/ButtonGroup";
import { WorkflowBuilder } from "../components/builders/WorkflowBuilder";
import { Dialog } from "../components/containers/Dialog";
import { Combobox } from "../components/fields/Combobox";
import { DatePicker } from "../components/fields/DatePicker";
import { Field } from "../components/fields/Field";
import { TextInput } from "../components/fields/TextInput";
import { Textarea } from "../components/fields/Textarea";
import { WriteOnlySecretField } from "../components/fields/WriteOnlySecretField";
import { TagPicker } from "../components/fields/TagPicker";
import { TagChip } from "../components/data/TagChip";
import { StatePanel } from "../components/feedback/StatePanel";
import { ConnectionCard } from "../patterns/ConnectionCard";
import { DurableJobProgress } from "../patterns/DurableJobProgress";
import "./catalog.css";

const sections = [
  "Foundations",
  "Components",
  "Patterns",
  "Templates",
  "States",
];

const classificationGroups = [
  { id: "technology", label: "Technology", description: "" },
  { id: "service", label: "Service", description: "" },
];
const classificationTags = [
  {
    id: "vpn",
    label: "VPN",
    groupId: "technology",
    state: "active" as const,
    synonyms: ["Remote access"],
    version: 1,
  },
  {
    id: "m365",
    label: "M365",
    groupId: "technology",
    state: "active" as const,
    synonyms: ["Microsoft 365"],
    version: 1,
  },
  {
    id: "billing",
    label: "Billing",
    groupId: "service",
    state: "active" as const,
    synonyms: [],
    version: 1,
  },
  {
    id: "retired",
    label: "Legacy backup",
    groupId: "technology",
    state: "archived" as const,
    synonyms: [],
    version: 1,
  },
];

export function DesignSystemCatalog() {
  const mainContentID = useMainContentID();
  const [density, setDensity] = useState<"compact" | "comfortable">(
    "comfortable",
  );
  const [dialogOpen, setDialogOpen] = useState(false);
  const [dueDate, setDueDate] = useState("2026-08-08");
  const [technician, setTechnician] = useState("maya");
  const [note, setNote] = useState(
    "Confirmed the policy scope and captured the affected users.",
  );
  const [classificationIDs, setClassificationIDs] = useState<string[]>(["vpn"]);

  return (
    <main
      id={mainContentID}
      className="rti-catalog"
      aria-labelledby="catalog-title"
    >
      <header className="rti-catalog__header">
        <div>
          <p className="rti-catalog__eyebrow">Authenticated product</p>
          <h1 id="catalog-title">Rarity design system</h1>
          <p>
            Accessible foundations and operational patterns for the Rarity PSA.
          </p>
        </div>
        <div className="rti-catalog__controls">
          <label className="rti-catalog__density">
            <span>Preview density</span>
            <select
              value={density}
              onChange={(event) =>
                setDensity(event.target.value as "compact" | "comfortable")
              }
            >
              <option value="comfortable">Comfortable</option>
              <option value="compact">Compact</option>
            </select>
          </label>
        </div>
      </header>

      <nav className="rti-catalog__navigation" aria-label="Catalog sections">
        {sections.map((section) => (
          <a key={section} href={`#catalog-${section.toLowerCase()}`}>
            {section}
          </a>
        ))}
      </nav>

      <div
        className="rti-catalog__preview"
        data-density={density}
        data-testid="catalog-preview"
      >
        <CatalogSection
          id="foundations"
          title="Foundations"
          description="Semantic color, type, spacing, density, focus, motion, and responsive contracts."
        >
          <div className="rti-catalog__foundation-grid">
            <div className="rti-catalog__surface" data-surface="canvas">
              <strong>Canvas</strong>
              <span>Persistent workspace background</span>
            </div>
            <div className="rti-catalog__surface" data-surface="panel">
              <strong>Panel surface</strong>
              <span>Grouped operational content</span>
            </div>
            <div className="rti-catalog__surface" data-surface="raised">
              <strong>Raised surface</strong>
              <span>Selected and movable work</span>
            </div>
            <div className="rti-catalog__surface" data-surface="floating">
              <strong>Floating surface</strong>
              <span>Menus, previews, and contextual tools</span>
            </div>
          </div>
        </CatalogSection>
        <CatalogSection
          id="components"
          title="Components"
          description="Actions, fields, navigation, containers, feedback, and data display."
        >
          <div className="rti-catalog__stack">
            <ButtonGroup label="Example actions">
              <Button intent="primary">Create work</Button>
              <Button>Review details</Button>
              <Button intent="danger">Revoke key</Button>
              <Button loading loadingLabel="Saving changes">
                Save
              </Button>
            </ButtonGroup>
            <Button onClick={() => setDialogOpen(true)}>Open dialog</Button>
            <div className="rti-catalog__field-grid">
              <Field
                id="catalog-connection-name"
                label="Connection name"
                hint="Use a name technicians will recognize."
                required
              >
                <TextInput />
              </Field>
              <WriteOnlySecretField
                id="catalog-api-key"
                label="API key"
                name="credential"
                configured
              />
              <DatePicker
                label="Due date"
                value={dueDate}
                onChange={setDueDate}
              />
              <Combobox
                label="Assigned technician"
                value={technician}
                onChange={setTechnician}
                options={[
                  {
                    value: "maya",
                    label: "Maya Chen",
                    description: "Service desk lead · available",
                  },
                  {
                    value: "jordan",
                    label: "Jordan Ellis",
                    description: "Senior technician · focused",
                  },
                ]}
              />
              <Field id="catalog-note" label="Internal note">
                <Textarea
                  value={note}
                  onChange={(event) => setNote(event.target.value)}
                />
              </Field>
            </div>
            <section
              className="rti-catalog__classification"
              aria-label="Classification component examples"
            >
              <h3>Classification controls</h3>
              <TagPicker
                label="Default classification"
                groups={classificationGroups}
                tags={classificationTags}
                selectedIds={classificationIDs}
                onChange={setClassificationIDs}
              />
              <div aria-busy="true" aria-label="Loading classification">
                Loading classification…
              </div>
              <TagPicker
                label="Suggested classification"
                groups={classificationGroups}
                tags={classificationTags}
                selectedIds={[]}
                suggestions={[
                  { id: "suggestion-vpn", tagId: "vpn", confidence: 0.98 },
                ]}
                onChange={() => {}}
                onSuggestionDecision={() => {}}
              />
              <TagPicker
                label="Inherited classification"
                groups={classificationGroups}
                tags={classificationTags}
                selectedIds={[]}
                inheritedIds={["m365"]}
                onChange={() => {}}
              />
              <TagPicker
                label="Required classification"
                groups={classificationGroups}
                tags={classificationTags}
                selectedIds={[]}
                required
                error="Select at least one tag."
                onChange={() => {}}
              />
              <div>
                <strong>Archived classification</strong>
                <TagChip tag={classificationTags[3]} />
              </div>
              <TagPicker
                label="Disabled classification"
                groups={classificationGroups}
                tags={classificationTags}
                selectedIds={["billing"]}
                disabled
                onChange={() => {}}
              />
            </section>
          </div>
        </CatalogSection>
        <CatalogSection
          id="patterns"
          title="Patterns"
          description="Scoped actions, versioned edits, durable jobs, connections, approvals, and audit evidence."
        >
          <div className="rti-catalog__state-grid">
            <ConnectionCard
              name="Local Ollama"
              providerType="Ollama"
              endpoint="http://host.docker.internal:11434"
              enabled
              health="healthy"
              credentialConfigured={false}
              credentialRequired={false}
              actions={
                <ButtonGroup label="Ollama connection actions">
                  <Button>Test connection</Button>
                  <Button>Discover models</Button>
                </ButtonGroup>
              }
            />
            <DurableJobProgress
              state="running"
              label="Discovering models"
              startedAt="Today, 5:20 PM"
              progress={{ completed: 3, total: 8 }}
              actions={<Button>Cancel discovery</Button>}
            />
          </div>
        </CatalogSection>
        <CatalogSection
          id="templates"
          title="Templates"
          description="Worklist, record, settings, dashboard, and wizard page structures."
        >
          <SignalTemplate />
          <WorkflowBuilder
            definition={{
              states: [
                { key: "triage", requires_owner: true },
                { key: "in_progress", sla_behavior: "active" },
                { key: "waiting", sla_behavior: "paused" },
              ],
              transitions: [
                { from: "triage", to: "in_progress" },
                { from: "in_progress", to: "waiting" },
              ],
            }}
          />
        </CatalogSection>
        <CatalogSection
          id="states"
          title="States"
          description="Loading, empty, error, permission, conflict, and environment recovery."
        >
          <div className="rti-catalog__state-grid">
            <StatePanel
              state="empty"
              title="No work matches these filters"
              description="Reset filters to see all assigned and unassigned work."
            />
            <StatePanel
              state="error"
              title="Work could not be loaded"
              description="Try the request again."
              action={<Button>Retry</Button>}
              supportCode="WORK-LIST-UNAVAILABLE"
            />
            <StatePanel
              state="permission"
              title="Additional access required"
              description="Work administration is required for this action."
            />
            <StatePanel
              state="conflict"
              title="This record changed"
              description="Reload the current version before applying your edit."
            />
          </div>
        </CatalogSection>
      </div>
      <Dialog
        open={dialogOpen}
        title="Review provider connection"
        description="Confirm the endpoint and scope before saving."
        onClose={() => setDialogOpen(false)}
        actions={
          <Button intent="primary" onClick={() => setDialogOpen(false)}>
            Done
          </Button>
        }
      >
        <p>The connection remains provider agnostic and client scoped.</p>
      </Dialog>
    </main>
  );
}

function SignalTemplate() {
  return (
    <div
      className="rti-catalog__signal-template"
      aria-label="Signal console template"
    >
      <nav aria-label="Signal template views">
        <strong>Queue views</strong>
        <button type="button">
          <ClipboardList size={15} aria-hidden="true" /> My work <b>8</b>
        </button>
        <button type="button">
          <ClipboardList size={15} aria-hidden="true" /> SLA risk <b>3</b>
        </button>
      </nav>
      <section>
        <header>
          <strong>Incident queue</strong>
          <Button size="compact">Assign</Button>
        </header>
        {[
          ["INC-1048", "Password resets failing", "Critical"],
          ["INC-1044", "VPN latency at clinic", "High"],
          ["REQ-1039", "New starter access", "Medium"],
        ].map(([id, title, priority]) => (
          <button type="button" key={id}>
            <b>{id}</b>
            <span>{title}</span>
            <em>{priority}</em>
          </button>
        ))}
      </section>
      <aside>
        <header>
          <PanelRight size={16} aria-hidden="true" />
          <strong>Inspector</strong>
        </header>
        <p className="rti-eyebrow">INC-1048</p>
        <h3>Password resets failing</h3>
        <p>Policy rollout · Aster & Vale Legal</p>
        <Button intent="primary" size="compact">
          Start work
        </Button>
      </aside>
    </div>
  );
}

function CatalogSection({
  id,
  title,
  description,
  children,
}: {
  id: string;
  title: string;
  description: string;
  children?: React.ReactNode;
}) {
  return (
    <section id={`catalog-${id}`} className="rti-catalog__section">
      <h2>{title}</h2>
      <p>{description}</p>
      {children ?? (
        <div className="rti-catalog__example">
          <strong>Examples arrive with each implementation phase.</strong>
          <span>
            Keyboard behavior, accessible naming, long text, and narrow layouts
            are documented beside the rendered component.
          </span>
        </div>
      )}
    </section>
  );
}
