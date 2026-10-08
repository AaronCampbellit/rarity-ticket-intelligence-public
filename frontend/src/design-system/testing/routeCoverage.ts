import { routeManifest } from "../../app/routes";

export function assertRouteCoverage(
  renderedRoutes: ReadonlySet<string>,
): string[] {
  return routeManifest
    .filter((route) => route.protected)
    .filter((route) => !renderedRoutes.has(route.id))
    .map((route) => route.id);
}
