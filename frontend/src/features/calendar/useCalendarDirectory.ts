import { useEffect, useState } from "react";
import { loadDirectory } from "../../api/browserSession";
import { array, record, string } from "./api";
import { calendarRequest, calendarError } from "./requests";
import type { Option } from "./formSupport";
export function useCalendarDirectory(enabled = true) {
  const [technicians, setTechnicians] = useState<Option[]>([]),
    [teams, setTeams] = useState<Option[]>([]),
    [names, setNames] = useState<Record<string, string>>({}),
    [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    if (enabled)
      void loadDirectory(controller.signal)
        .then((value) => {
          const data = record(value),
            parse = (key: string) =>
              array(data[key]).map((value) => {
                const row = record(value);
                return {
                  id: string(row.id),
                  name: string(row.display_name || row.name),
                };
              });
          const techs = parse("technicians"),
            teams = parse("teams");
          if (!controller.signal.aborted) {
            setTechnicians(techs);
            setTeams(teams);
            setNames(
              Object.fromEntries(
                [
                  ...techs,
                  ...teams,
                  ...parse("departments"),
                  ...parse("queues"),
                  ...parse("clients"),
                ].map((item) => [item.id, item.name]),
              ),
            );
          }
        })
        .catch((error) => {
          if (!controller.signal.aborted) setError(calendarError(error));
        });
    return () => controller.abort();
  }, [enabled]);
  return { technicians, teams, names, error };
}
export function useOptions(
  path?: string,
  clientID?: string,
  collection?: string,
) {
  const [options, setOptions] = useState<Array<Option & { kind?: string }>>([]),
    [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    setOptions([]);
    setError("");
    if (path)
      void calendarRequest(path, { clientID, signal: controller.signal })
        .then((value) => {
          const list = array(
            collection ? record(value)[collection] : value,
          ).map((value) => {
            const row = record(value);
            return {
              id: string(row.id),
              name: string(row.name || row.title || row.display_name),
              kind: typeof row.kind === "string" ? row.kind : undefined,
            };
          });
          if (!controller.signal.aborted) setOptions(list);
        })
        .catch((error) => {
          if (!controller.signal.aborted) setError(calendarError(error));
        });
    return () => controller.abort();
  }, [path, clientID, collection]);
  return { options, error };
}
