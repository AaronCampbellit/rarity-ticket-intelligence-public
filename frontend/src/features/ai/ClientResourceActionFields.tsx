import type { ResourceFormState } from "./structuredAction";
import type { ClientResourceKind, ClientResourceSummary } from "./types";

export function ClientResourceFields({
  kind,
  form,
  locations,
  locationError,
  showLocation,
  onChange,
}: {
  kind: ClientResourceKind;
  form: ResourceFormState;
  locations: ClientResourceSummary[];
  locationError: string;
  showLocation: boolean;
  onChange: (field: keyof ResourceFormState, value: string) => void;
}) {
  const locationField =
    showLocation && (kind === "contact" || kind === "asset") ? (
      <label>
        Location
        <select
          value={form.location}
          onChange={(event) => onChange("location", event.target.value)}
          disabled={Boolean(locationError)}
        >
          <option value="">No location</option>
          {locations.map((location) => (
            <option key={location.id} value={location.display_id}>
              {location.name} ({location.display_id})
            </option>
          ))}
        </select>
        {locationError ? <small>{locationError}</small> : null}
      </label>
    ) : null;
  return (
    <>
      <div className="ai-workspace__action-grid">
        <label>
          Display ID
          <input
            value={form.displayID}
            onChange={(event) => onChange("displayID", event.target.value)}
            required
          />
        </label>
        {kind === "asset" ? (
          <label>
            Asset type
            <input
              value={form.assetType}
              onChange={(event) => onChange("assetType", event.target.value)}
              required
            />
          </label>
        ) : null}
      </div>
      <label>
        {kind === "contact" ? "Display name" : "Name"}
        <input
          value={form.name}
          onChange={(event) => onChange("name", event.target.value)}
          required
        />
      </label>
      {kind === "contact" ? (
        <>
          <div className="ai-workspace__action-grid">
            <label>
              Email
              <input
                type="email"
                value={form.email}
                onChange={(event) => onChange("email", event.target.value)}
              />
            </label>
            <label>
              Phone
              <input
                value={form.phone}
                onChange={(event) => onChange("phone", event.target.value)}
              />
            </label>
          </div>
          {locationField}
        </>
      ) : null}
      {kind === "asset" ? (
        <>
          {locationField}
          <div className="ai-workspace__action-grid">
            <label>
              Source system
              <input
                value={form.sourceSystem}
                onChange={(event) =>
                  onChange("sourceSystem", event.target.value)
                }
                required={Boolean(form.externalID.trim())}
              />
            </label>
            <label>
              External ID
              <input
                value={form.externalID}
                onChange={(event) => onChange("externalID", event.target.value)}
                required={Boolean(form.sourceSystem.trim())}
              />
            </label>
          </div>
        </>
      ) : null}
      {kind === "service" ? (
        <label>
          Criticality
          <select
            value={form.criticality}
            onChange={(event) => onChange("criticality", event.target.value)}
          >
            <option value="">Not set</option>
            <option value="low">Low</option>
            <option value="normal">Normal</option>
            <option value="high">High</option>
            <option value="critical">Critical</option>
          </select>
        </label>
      ) : null}
      {kind === "contract" ? (
        <div className="ai-workspace__action-grid">
          <label>
            Starts on
            <input
              type="date"
              value={form.startsOn}
              onChange={(event) => onChange("startsOn", event.target.value)}
              required
            />
          </label>
          <label>
            Ends on
            <input
              type="date"
              min={form.startsOn || undefined}
              value={form.endsOn}
              onChange={(event) => onChange("endsOn", event.target.value)}
            />
          </label>
        </div>
      ) : null}
    </>
  );
}

export function ClientResourceUpdateFields({
  kind,
  form,
  clearFields,
  onChange,
  onClear,
}: {
  kind: ClientResourceKind;
  form: ResourceFormState;
  clearFields: Set<string>;
  onChange: (field: keyof ResourceFormState, value: string) => void;
  onClear: (field: keyof ResourceFormState, checked: boolean) => void;
}) {
  const optionalField = (
    field: keyof ResourceFormState,
    fieldLabel: string,
    type?: string,
  ) => (
    <fieldset className="ai-workspace__optional-field">
      <label>
        {fieldLabel}
        <input
          type={type}
          value={form[field]}
          disabled={clearFields.has(field)}
          onChange={(event) => onChange(field, event.target.value)}
        />
      </label>
      <label className="ai-workspace__clear-field">
        <input
          type="checkbox"
          checked={clearFields.has(field)}
          onChange={(event) => onClear(field, event.target.checked)}
        />
        Clear {fieldLabel.toLowerCase()}
      </label>
    </fieldset>
  );
  return (
    <section
      className="ai-workspace__resource-patch"
      aria-label={`${resourceKindLabel(kind)} update fields`}
    >
      {kind === "location" ? (
        <label>
          Name
          <input
            value={form.name}
            onChange={(event) => onChange("name", event.target.value)}
          />
        </label>
      ) : null}
      {kind === "contact" ? (
        <>
          <label>
            Display name
            <input
              value={form.name}
              onChange={(event) => onChange("name", event.target.value)}
            />
          </label>
          {optionalField("email", "Email", "email")}
          {optionalField("phone", "Phone")}
          {optionalField("location", "Location")}
        </>
      ) : null}
      {kind === "asset" ? (
        <>
          <label>
            Name
            <input
              value={form.name}
              onChange={(event) => onChange("name", event.target.value)}
            />
          </label>
          <label>
            Asset type
            <input
              value={form.assetType}
              onChange={(event) => onChange("assetType", event.target.value)}
            />
          </label>
          {optionalField("location", "Location")}
        </>
      ) : null}
      {kind === "service" ? (
        <>
          <label>
            Name
            <input
              value={form.name}
              onChange={(event) => onChange("name", event.target.value)}
            />
          </label>
          <label>
            Criticality
            <select
              value={form.criticality}
              onChange={(event) => onChange("criticality", event.target.value)}
            >
              <option value="">No change</option>
              <option value="low">Low</option>
              <option value="normal">Normal</option>
              <option value="high">High</option>
              <option value="critical">Critical</option>
            </select>
          </label>
        </>
      ) : null}
      {kind === "contract" ? (
        <>
          <label>
            Name
            <input
              value={form.name}
              onChange={(event) => onChange("name", event.target.value)}
            />
          </label>
          <label>
            Starts on
            <input
              type="date"
              value={form.startsOn}
              onChange={(event) => onChange("startsOn", event.target.value)}
            />
          </label>
          <fieldset className="ai-workspace__optional-field">
            <label>
              Ends on
              <input
                type="date"
                value={form.endsOn}
                disabled={clearFields.has("endsOn")}
                onChange={(event) => onChange("endsOn", event.target.value)}
              />
            </label>
            <label className="ai-workspace__clear-field">
              <input
                type="checkbox"
                checked={clearFields.has("endsOn")}
                onChange={(event) => onClear("endsOn", event.target.checked)}
              />
              Clear end date
            </label>
          </fieldset>
        </>
      ) : null}
    </section>
  );
}

function resourceKindLabel(kind: ClientResourceKind): string {
  return kind[0].toUpperCase() + kind.slice(1);
}
