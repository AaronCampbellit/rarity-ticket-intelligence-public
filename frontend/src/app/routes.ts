export type RouteID =
  | "home"
  | "work"
  | "calendar"
  | "timesheets"
  | "sales"
  | "proposal"
  | "conversion"
  | "project"
  | "ai-settings"
  | "ai-assist"
  | "teams-settings"
  | "sessions"
  | "recovery-access"
  | "role-settings"
  | "audit"
  | "service-desk-settings"
  | "classification-settings"
  | "classification-insights"
  | "setup"
  | "operations"
  | "automation"
  | "service-keys"
  | "webhooks"
  | "datto-reconciliation"
  | "datto-settings"
  | "forwarding-settings"
  | "graph-settings"
  | "knowledge"
  | "billing"
  | "directory-settings"
  | "client-resources"
  | "pipeline-settings"
  | "prospects"
  | "design-system"
  | "login"
  | "break-glass";

export type ProductRole = "technician" | "admin" | "sales" | "delivery";
export type PageFamily =
  | "home"
  | "worklist"
  | "record"
  | "settings"
  | "operations"
  | "public"
  | "catalog";
export type ProductGroup =
  | "home"
  | "work"
  | "sales"
  | "delivery"
  | "clients"
  | "knowledge"
  | "operations"
  | "admin";
export type NavigationGroup =
  | "home"
  | "work"
  | "sales"
  | "delivery"
  | "organization"
  | "admin-access"
  | "admin-platform"
  | "admin-operations"
  | "admin-integrations";

export type RouteDefinition = {
  id: RouteID;
  label: string;
  productGroup: ProductGroup;
  navigationGroup: NavigationGroup | null;
  family: PageFamily;
  roles: ProductRole[];
  density: "compact" | "comfortable";
  mobile: boolean;
  protected: boolean;
  catalogOnly?: boolean;
};

const everyone: ProductRole[] = ["technician", "admin", "sales", "delivery"];
const operators: ProductRole[] = ["technician", "admin", "delivery"];
const commercial: ProductRole[] = ["sales", "admin"];
const administrators: ProductRole[] = ["admin"];

export const routeManifest: RouteDefinition[] = [
  {
    id: "calendar",
    label: "Calendar",
    productGroup: "work",
    navigationGroup: "work",
    family: "worklist",
    roles: everyone,
    density: "compact",
    mobile: false,
    protected: true,
  },
  {
    id: "home",
    label: "Home",
    productGroup: "home",
    navigationGroup: "home",
    family: "home",
    roles: everyone,
    density: "comfortable",
    mobile: true,
    protected: true,
  },
  {
    id: "work",
    label: "Work",
    productGroup: "work",
    navigationGroup: "work",
    family: "worklist",
    roles: operators,
    density: "compact",
    mobile: true,
    protected: true,
  },
  {
    id: "timesheets",
    label: "Timesheets",
    productGroup: "work",
    navigationGroup: "work",
    family: "worklist",
    roles: operators,
    density: "compact",
    mobile: true,
    protected: true,
  },
  {
    id: "sales",
    label: "Sales",
    productGroup: "sales",
    navigationGroup: "sales",
    family: "worklist",
    roles: commercial,
    density: "compact",
    mobile: false,
    protected: true,
  },
  {
    id: "proposal",
    label: "Proposals",
    productGroup: "sales",
    navigationGroup: "sales",
    family: "worklist",
    roles: commercial,
    density: "compact",
    mobile: false,
    protected: true,
  },
  {
    id: "conversion",
    label: "Conversion",
    productGroup: "sales",
    navigationGroup: "sales",
    family: "record",
    roles: commercial,
    density: "comfortable",
    mobile: false,
    protected: true,
  },
  {
    id: "prospects",
    label: "Prospects",
    productGroup: "sales",
    navigationGroup: "sales",
    family: "worklist",
    roles: commercial,
    density: "compact",
    mobile: false,
    protected: true,
  },
  {
    id: "project",
    label: "Projects",
    productGroup: "delivery",
    navigationGroup: "delivery",
    family: "worklist",
    roles: ["delivery", "technician", "admin"],
    density: "compact",
    mobile: false,
    protected: true,
  },
  {
    id: "knowledge",
    label: "Knowledge",
    productGroup: "knowledge",
    navigationGroup: "organization",
    family: "worklist",
    roles: everyone,
    density: "compact",
    mobile: true,
    protected: true,
  },
  {
    id: "directory-settings",
    label: "Clients and directory",
    productGroup: "clients",
    navigationGroup: "organization",
    family: "settings",
    roles: administrators,
    density: "comfortable",
    mobile: false,
    protected: true,
  },
  {
    id: "client-resources",
    label: "Client resources",
    productGroup: "clients",
    navigationGroup: "organization",
    family: "worklist",
    roles: operators,
    density: "compact",
    mobile: false,
    protected: true,
  },
  {
    id: "operations",
    label: "Operations",
    productGroup: "operations",
    navigationGroup: "admin-operations",
    family: "operations",
    roles: administrators,
    density: "compact",
    mobile: false,
    protected: true,
  },
  {
    id: "datto-reconciliation",
    label: "Datto reconciliation",
    productGroup: "operations",
    navigationGroup: "admin-operations",
    family: "worklist",
    roles: administrators,
    density: "compact",
    mobile: false,
    protected: true,
  },
  {
    id: "forwarding-settings",
    label: "Forwarding intake",
    productGroup: "operations",
    navigationGroup: "admin-operations",
    family: "settings",
    roles: administrators,
    density: "comfortable",
    mobile: false,
    protected: true,
  },
  {
    id: "ai-assist",
    label: "AI assist",
    productGroup: "operations",
    navigationGroup: "admin-operations",
    family: "operations",
    roles: operators,
    density: "comfortable",
    mobile: true,
    protected: true,
  },
  {
    id: "sessions",
    label: "Sessions",
    productGroup: "admin",
    navigationGroup: "admin-access",
    family: "worklist",
    roles: administrators,
    density: "compact",
    mobile: false,
    protected: true,
  },
  {
    id: "recovery-access",
    label: "Local administrators",
    productGroup: "admin",
    navigationGroup: "admin-access",
    family: "settings",
    roles: administrators,
    density: "comfortable",
    mobile: false,
    protected: true,
  },
  {
    id: "role-settings",
    label: "Roles",
    productGroup: "admin",
    navigationGroup: "admin-access",
    family: "settings",
    roles: administrators,
    density: "comfortable",
    mobile: false,
    protected: true,
  },
  {
    id: "audit",
    label: "Audit",
    productGroup: "admin",
    navigationGroup: "admin-access",
    family: "worklist",
    roles: administrators,
    density: "compact",
    mobile: false,
    protected: true,
  },
  {
    id: "service-desk-settings",
    label: "Service desk settings",
    productGroup: "admin",
    navigationGroup: "admin-platform",
    family: "settings",
    roles: administrators,
    density: "comfortable",
    mobile: false,
    protected: true,
  },
  {
    id: "classification-settings",
    label: "Classification settings",
    productGroup: "admin",
    navigationGroup: "admin-platform",
    family: "settings",
    roles: administrators,
    density: "comfortable",
    mobile: true,
    protected: true,
  },
  {
    id: "classification-insights",
    label: "Classification insights",
    productGroup: "admin",
    navigationGroup: "admin-operations",
    family: "operations",
    roles: administrators,
    density: "compact",
    mobile: false,
    protected: true,
  },
  {
    id: "pipeline-settings",
    label: "Sales pipelines",
    productGroup: "admin",
    navigationGroup: "admin-platform",
    family: "settings",
    roles: administrators,
    density: "comfortable",
    mobile: false,
    protected: true,
  },
  {
    id: "setup",
    label: "Setup center",
    productGroup: "admin",
    navigationGroup: "admin-platform",
    family: "settings",
    roles: administrators,
    density: "comfortable",
    mobile: false,
    protected: true,
  },
  {
    id: "service-keys",
    label: "Service API keys",
    productGroup: "admin",
    navigationGroup: "admin-platform",
    family: "settings",
    roles: administrators,
    density: "comfortable",
    mobile: false,
    protected: true,
  },
  {
    id: "billing",
    label: "Billing review",
    productGroup: "admin",
    navigationGroup: "admin-platform",
    family: "worklist",
    roles: administrators,
    density: "compact",
    mobile: true,
    protected: true,
  },
  {
    id: "ai-settings",
    label: "AI settings",
    productGroup: "admin",
    navigationGroup: "admin-operations",
    family: "settings",
    roles: administrators,
    density: "comfortable",
    mobile: false,
    protected: true,
  },
  {
    id: "teams-settings",
    label: "Teams settings",
    productGroup: "admin",
    navigationGroup: "admin-integrations",
    family: "settings",
    roles: administrators,
    density: "comfortable",
    mobile: false,
    protected: true,
  },
  {
    id: "datto-settings",
    label: "Datto connections",
    productGroup: "admin",
    navigationGroup: "admin-integrations",
    family: "settings",
    roles: administrators,
    density: "comfortable",
    mobile: false,
    protected: true,
  },
  {
    id: "graph-settings",
    label: "Graph mailboxes",
    productGroup: "admin",
    navigationGroup: "admin-integrations",
    family: "settings",
    roles: administrators,
    density: "comfortable",
    mobile: false,
    protected: true,
  },
  {
    id: "webhooks",
    label: "Webhooks",
    productGroup: "admin",
    navigationGroup: "admin-integrations",
    family: "settings",
    roles: administrators,
    density: "comfortable",
    mobile: false,
    protected: true,
  },
  {
    id: "automation",
    label: "Automation",
    productGroup: "admin",
    navigationGroup: "admin-integrations",
    family: "operations",
    roles: administrators,
    density: "compact",
    mobile: false,
    protected: true,
  },
  {
    id: "design-system",
    label: "Design system",
    productGroup: "admin",
    navigationGroup: "admin-platform",
    family: "catalog",
    roles: administrators,
    density: "comfortable",
    mobile: false,
    protected: true,
    catalogOnly: true,
  },
  {
    id: "login",
    label: "Sign in",
    productGroup: "home",
    navigationGroup: null,
    family: "public",
    roles: everyone,
    density: "comfortable",
    mobile: true,
    protected: false,
  },
  {
    id: "break-glass",
    label: "Recovery access",
    productGroup: "home",
    navigationGroup: null,
    family: "public",
    roles: administrators,
    density: "comfortable",
    mobile: true,
    protected: false,
  },
];

const routeByID = new Map(routeManifest.map((route) => [route.id, route]));
const nestedRoutePrefixes: Array<{ prefix: string; routeID: RouteID }> = [
  { prefix: "sales/opportunities/", routeID: "sales" },
];

export function routeIDFromPath(path: string): RouteID | undefined {
  if (routeByID.has(path as RouteID)) return path as RouteID;
  return nestedRoutePrefixes.find(({ prefix }) => path.startsWith(prefix))
    ?.routeID;
}

export function routeForID(id: RouteID): RouteDefinition {
  return routeByID.get(id) ?? routeByID.get("home")!;
}

export function routeForHash(
  hash: string,
  catalogEnabled: boolean,
): RouteDefinition {
  const path = hash.replace(/^#\//, "").split("?", 1)[0];
  const routeID = routeIDFromPath(path);
  const route = routeID ? routeByID.get(routeID) : undefined;
  if (!route || (route.catalogOnly && !catalogEnabled)) {
    return routeByID.get("home")!;
  }
  return route;
}

export function protectedNavigationRoutes(
  catalogEnabled: boolean,
): RouteDefinition[] {
  return routeManifest.filter(
    (route) =>
      route.protected &&
      route.navigationGroup &&
      (!route.catalogOnly || catalogEnabled),
  );
}
