import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";

export type Destination = {
  channel: "in_app" | "email" | "teams" | "webhook";
  recipient_ref: string;
  content_classification: string;
};

export function DestinationBuilder({
  destinations,
  allowedChannels = ["in_app", "email", "teams", "webhook"],
}: {
  destinations: Destination[];
  allowedChannels?: Destination["channel"][];
}) {
  const [rows, setRows] = useState(destinations);
  return (
    <fieldset className="rti-builder">
      <legend>Delivery destinations</legend>
      <p>
        Choose where the notification goes and what information it may contain.
      </p>
      <div className="rti-builder__rows">
        {rows.map((row, index) => (
          <div className="rti-builder__row" key={`${row.channel}-${index}`}>
            <span>{index + 1}</span>
            <label>
              <span>Channel</span>
              <select
                name="destination_channel"
                value={row.channel}
                onChange={(event) =>
                  setRows((current) =>
                    current.map((item, itemIndex) =>
                      itemIndex === index
                        ? {
                            ...item,
                            channel: event.target
                              .value as Destination["channel"],
                          }
                        : item,
                    ),
                  )
                }
              >
                {allowedChannels.includes("in_app") ? (
                  <option value="in_app">In-app</option>
                ) : null}
                {allowedChannels.includes("email") ? (
                  <option value="email">Email</option>
                ) : null}
                {allowedChannels.includes("teams") ? (
                  <option value="teams">Microsoft Teams</option>
                ) : null}
                {allowedChannels.includes("webhook") ? (
                  <option value="webhook">Webhook</option>
                ) : null}
              </select>
            </label>
            <label>
              <span>Recipient or connection</span>
              <input
                name="destination_recipient"
                value={row.recipient_ref}
                onChange={(event) =>
                  setRows((current) =>
                    current.map((item, itemIndex) =>
                      itemIndex === index
                        ? { ...item, recipient_ref: event.target.value }
                        : item,
                    ),
                  )
                }
                required
              />
            </label>
            <label>
              <span>Content level</span>
              <select
                name="destination_classification"
                value={row.content_classification}
                onChange={(event) =>
                  setRows((current) =>
                    current.map((item, itemIndex) =>
                      itemIndex === index
                        ? {
                            ...item,
                            content_classification: event.target.value,
                          }
                        : item,
                    ),
                  )
                }
              >
                <option value="internal">Internal</option>
                <option value="restricted">Restricted</option>
                <option value="public">Public-safe</option>
              </select>
            </label>
            <button
              type="button"
              aria-label={`Remove destination ${index + 1}`}
              onClick={() =>
                setRows((current) =>
                  current.filter((_, itemIndex) => itemIndex !== index),
                )
              }
            >
              <Trash2 size={15} aria-hidden="true" />
            </button>
          </div>
        ))}
      </div>
      <button
        className="rti-builder__add"
        type="button"
        onClick={() =>
          setRows((current) => [
            ...current,
            {
              channel: allowedChannels[0] ?? "email",
              recipient_ref: allowedChannels[0] === "email" ? "recipient" : "",
              content_classification: "internal",
            },
          ])
        }
      >
        <Plus size={15} aria-hidden="true" /> Add destination
      </button>
    </fieldset>
  );
}

export function destinationsFromForm(data: FormData): Destination[] {
  const channels = data.getAll("destination_channel").map(String);
  const recipients = data.getAll("destination_recipient").map(String);
  const classifications = data.getAll("destination_classification").map(String);
  return channels.map((channel, index) => ({
    channel: channel as Destination["channel"],
    recipient_ref: recipients[index],
    content_classification: classifications[index],
  }));
}
