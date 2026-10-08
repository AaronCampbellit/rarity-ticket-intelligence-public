import {
  protectedNavigationRoutes,
  routeForHash,
  routeForID,
  routeIDFromPath,
  routeManifest,
  type NavigationGroup,
  type RouteID,
} from "./app/routes";

export type Page = RouteID;

export const navigation: Array<{ page: Page; label: string }> =
  protectedNavigationRoutes(false)
    .filter(({ id }) => id !== "design-system")
    .map(({ id, label }) => ({ page: id, label }));

export function navigationGroup(page: Page): NavigationGroup {
  return routeForID(page).navigationGroup ?? "admin-platform";
}

export function pageFromHash(hash: string, catalogEnabled: boolean): Page {
  return routeForHash(hash, catalogEnabled).id;
}

export function isRecognizedProtectedHash(
  hash: string,
  catalogEnabled: boolean,
): boolean {
  if (!hash.startsWith("#/")) return false;
  const candidate = hash.slice(2).split("?", 1)[0];
  const routeID = routeIDFromPath(candidate);
  const route = routeManifest.find(({ id }) => id === routeID);
  if (!route || route.id === "setup") return false;
  if (route.catalogOnly && !catalogEnabled) return false;
  return route.protected;
}

export type MentionNavigation = {
  routeID: "work" | "project";
  parentID: string;
  mentionOccurrenceID: string;
  sourceID?: string;
};

export function mentionNavigationFromHash(
  hash: string,
): MentionNavigation | undefined {
  if (!hash.startsWith("#/")) return undefined;
  const [path, query = ""] = hash.slice(2).split("?", 2);
  if (path !== "work" && path !== "project") return undefined;
  if (routeForHash(hash, false).id !== path) return undefined;
  const parameters = new URLSearchParams(query);
  const allowed = new Set(["parentID", "mentionOccurrenceID", "sourceID"]);
  for (const key of parameters.keys()) {
    if (!allowed.has(key) || parameters.getAll(key).length !== 1)
      return undefined;
  }
  const bounded = (name: string) => {
    const value = parameters.get(name)?.trim();
    return value && value.length <= 128 ? value : undefined;
  };
  const parentID = bounded("parentID");
  const mentionOccurrenceID = bounded("mentionOccurrenceID");
  const sourceID = bounded("sourceID");
  if (!parentID || !mentionOccurrenceID) return undefined;
  return {
    routeID: path,
    parentID,
    mentionOccurrenceID,
    ...(sourceID ? { sourceID } : {}),
  };
}
