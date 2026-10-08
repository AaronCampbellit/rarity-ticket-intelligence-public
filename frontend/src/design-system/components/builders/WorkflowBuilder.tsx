import { ArrowDown, ArrowRight, Plus, Trash2 } from "lucide-react";
import { useState } from "react";

type State = { key: string; requires_owner?: boolean; sla_behavior?: string };
type Transition = { from: string; to: string };

export function WorkflowBuilder({
  definition,
}: {
  definition: { states: State[]; transitions: Transition[] };
}) {
  const [states, setStates] = useState(() => definition.states);
  const [transitions, setTransitions] = useState(() => definition.transitions);
  return (
    <fieldset className="rti-builder rti-workflow-builder">
      <legend>States and transitions</legend>
      <p>
        Design the ticket lifecycle. Transitions connect one state to another.
      </p>
      <div className="rti-workflow-builder__states">
        {states.map((state, index) => (
          <div key={`${state.key}-${index}`}>
            <span>{index + 1}</span>
            <label>
              <span>State name</span>
              <input
                name="workflow_state"
                value={state.key}
                onChange={(event) =>
                  setStates((current) =>
                    current.map((item, itemIndex) =>
                      itemIndex === index
                        ? { ...item, key: event.target.value }
                        : item,
                    ),
                  )
                }
              />
            </label>
            <label>
              <span>SLA behavior</span>
              <select
                name="workflow_sla"
                value={state.sla_behavior ?? "active"}
                onChange={(event) =>
                  setStates((current) =>
                    current.map((item, itemIndex) =>
                      itemIndex === index
                        ? { ...item, sla_behavior: event.target.value }
                        : item,
                    ),
                  )
                }
              >
                <option value="active">Count time</option>
                <option value="paused">Pause time</option>
                <option value="resolved">Complete SLA</option>
              </select>
            </label>
            <label className="rti-builder__check">
              <input
                name="workflow_owner"
                type="checkbox"
                value={String(index)}
                checked={Boolean(state.requires_owner)}
                onChange={(event) =>
                  setStates((current) =>
                    current.map((item, itemIndex) =>
                      itemIndex === index
                        ? { ...item, requires_owner: event.target.checked }
                        : item,
                    ),
                  )
                }
              />
              Owner required
            </label>
            <button
              type="button"
              aria-label={`Remove ${state.key} state`}
              onClick={() =>
                setStates((current) =>
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
          setStates((current) => [
            ...current,
            { key: `state_${current.length + 1}`, sla_behavior: "active" },
          ])
        }
      >
        <Plus size={15} aria-hidden="true" /> Add state
      </button>
      <div className="rti-workflow-builder__transitions">
        <h3>Allowed transitions</h3>
        {transitions.map((transition, index) => (
          <div key={`${transition.from}-${transition.to}-${index}`}>
            <select
              name="transition_from"
              value={transition.from}
              aria-label={`Transition ${index + 1} from`}
              onChange={(event) =>
                setTransitions((current) =>
                  current.map((item, itemIndex) =>
                    itemIndex === index
                      ? { ...item, from: event.target.value }
                      : item,
                  ),
                )
              }
            >
              {states.map((state) => (
                <option key={state.key} value={state.key}>
                  {state.key.replaceAll("_", " ")}
                </option>
              ))}
            </select>
            <ArrowRight size={16} aria-label="to" />
            <select
              name="transition_to"
              value={transition.to}
              aria-label={`Transition ${index + 1} to`}
              onChange={(event) =>
                setTransitions((current) =>
                  current.map((item, itemIndex) =>
                    itemIndex === index
                      ? { ...item, to: event.target.value }
                      : item,
                  ),
                )
              }
            >
              {states.map((state) => (
                <option key={state.key} value={state.key}>
                  {state.key.replaceAll("_", " ")}
                </option>
              ))}
            </select>
            <button
              type="button"
              aria-label={`Remove transition ${index + 1}`}
              onClick={() =>
                setTransitions((current) =>
                  current.filter((_, itemIndex) => itemIndex !== index),
                )
              }
            >
              <Trash2 size={15} aria-hidden="true" />
            </button>
          </div>
        ))}
        <button
          className="rti-builder__add"
          type="button"
          disabled={states.length < 2}
          onClick={() =>
            setTransitions((current) => [
              ...current,
              { from: states[0]?.key ?? "", to: states[1]?.key ?? "" },
            ])
          }
        >
          <ArrowDown size={15} aria-hidden="true" /> Add transition
        </button>
      </div>
    </fieldset>
  );
}

export function workflowFromForm(data: FormData) {
  const states = data.getAll("workflow_state").map(String);
  const sla = data.getAll("workflow_sla").map(String);
  const owners = new Set(data.getAll("workflow_owner").map(Number));
  const from = data.getAll("transition_from").map(String);
  const to = data.getAll("transition_to").map(String);
  return {
    states: states.map((key, index) => ({
      key,
      requires_owner: owners.has(index) || undefined,
      sla_behavior: sla[index],
    })),
    transitions: from.map((value, index) => ({ from: value, to: to[index] })),
  };
}
