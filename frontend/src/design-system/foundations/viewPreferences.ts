export type SupportedWorkView = "list" | "kanban";
export type ViewPreferenceRoute = "work" | "sales" | "project";

export function viewPreferenceKey(
  principalID: string,
  routeID: ViewPreferenceRoute,
) {
  return `rti:view:${encodeURIComponent(principalID)}:${routeID}`;
}

export function readViewPreference(
  storage: Storage,
  principalID: string,
  routeID: ViewPreferenceRoute,
): SupportedWorkView {
  try {
    const value = JSON.parse(
      storage.getItem(viewPreferenceKey(principalID, routeID)) ?? '""',
    );
    return value === "list" || value === "kanban" ? value : "list";
  } catch {
    return "list";
  }
}

export function writeViewPreference(
  storage: Storage,
  principalID: string,
  routeID: ViewPreferenceRoute,
  view: SupportedWorkView,
): void {
  try {
    storage.setItem(
      viewPreferenceKey(principalID, routeID),
      JSON.stringify(view),
    );
  } catch {
    // View preferences are optional and must not block the active workflow.
  }
}
