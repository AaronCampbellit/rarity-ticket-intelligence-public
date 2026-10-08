import {
  Activity,
  Bot,
  BookOpen,
  Building2,
  ChevronDown,
  ChevronLeft,
  ChevronRight,
  ClipboardList,
  CalendarDays,
  DollarSign,
  FileText,
  FolderKanban,
  KeyRound,
  Mail,
  Settings,
  ShieldCheck,
  TrendingUp,
  Users,
  Webhook,
  type LucideIcon,
} from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";

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

export type NavigationItem = {
  id: string;
  label: string;
  href: string;
  group: NavigationGroup;
  onSelect?: () => void;
};

const groupLabels: Record<NavigationGroup, string> = {
  home: "Overview",
  work: "Work",
  sales: "Sales",
  delivery: "Delivery",
  organization: "Organization",
  "admin-access": "Access",
  "admin-platform": "Platform",
  "admin-operations": "Operations",
  "admin-integrations": "Integrations",
};

const groupOrder: NavigationGroup[] = [
  "home",
  "work",
  "sales",
  "delivery",
  "organization",
];

const adminGroupOrder: NavigationGroup[] = [
  "admin-access",
  "admin-platform",
  "admin-operations",
  "admin-integrations",
];

const navigationIcons: Record<string, LucideIcon> = {
  home: Activity,
  work: ClipboardList,
  calendar: CalendarDays,
  "service-desk-settings": Settings,
  sales: TrendingUp,
  proposal: FileText,
  conversion: Activity,
  "pipeline-settings": Settings,
  prospects: Users,
  project: FolderKanban,
  "ai-assist": Bot,
  "ai-settings": Bot,
  "teams-settings": Users,
  operations: Activity,
  automation: Activity,
  webhooks: Webhook,
  "datto-reconciliation": Activity,
  "datto-settings": Settings,
  "forwarding-settings": Mail,
  "graph-settings": Mail,
  sessions: ShieldCheck,
  "recovery-access": Users,
  "role-settings": ShieldCheck,
  audit: ClipboardList,
  setup: Settings,
  "service-keys": KeyRound,
  knowledge: BookOpen,
  billing: DollarSign,
  "directory-settings": Building2,
  "client-resources": Building2,
  "design-system": Settings,
};

export function navigationStorageKey(principalID: string) {
  return `rti:navigation:${encodeURIComponent(principalID)}`;
}

function readCollapsedNavigation(principalID: string) {
  try {
    const parsed = JSON.parse(
      window.localStorage.getItem(navigationStorageKey(principalID)) ?? "{}",
    ) as { collapsed?: unknown };
    return parsed.collapsed === true;
  } catch {
    return false;
  }
}

export function GroupedSidebar({
  brand,
  navigation,
  activeID,
  buildLabel,
  principalID,
  open,
  onNavigate,
}: {
  brand: ReactNode;
  navigation: NavigationItem[];
  activeID: string;
  buildLabel: string;
  principalID: string;
  open: boolean;
  onNavigate: () => void;
}) {
  const adminItems = navigation.filter((item) =>
    adminGroupOrder.includes(item.group),
  );
  const activeAdmin = adminItems.some((item) => item.id === activeID);
  const [adminOpen, setAdminOpen] = useState(activeAdmin);
  const [collapsed, setCollapsed] = useState(() =>
    readCollapsedNavigation(principalID),
  );

  useEffect(() => {
    if (activeAdmin) setAdminOpen(true);
  }, [activeAdmin]);

  function renderLink(item: NavigationItem) {
    const Icon = navigationIcons[item.id] ?? ClipboardList;
    return (
      <a
        key={item.id}
        href={item.href}
        className={activeID === item.id ? "active" : ""}
        aria-current={activeID === item.id ? "page" : undefined}
        aria-label={item.label}
        title={collapsed ? item.label : undefined}
        onClick={() => {
          item.onSelect?.();
          onNavigate();
        }}
      >
        <Icon size={16} strokeWidth={1.7} aria-hidden="true" />
        <span>{item.label}</span>
      </a>
    );
  }

  return (
    <aside
      id="rti-primary-sidebar"
      className="rarity-sidebar"
      data-open={open || undefined}
      data-collapsed={collapsed || undefined}
    >
      <div className="rarity-brand">
        <div className="rarity-brand__identity">{brand}</div>
        <a
          className="rarity-brand__compact"
          href="#/home"
          aria-label="Rarity home"
          title="Rarity home"
          onClick={() => {
            navigation.find(({ id }) => id === "home")?.onSelect?.();
            onNavigate();
          }}
        >
          <img src="/brand/rarity-symbol.png" width="83" height="80" alt="" />
        </a>
        <button
          type="button"
          className="rti-navigation-collapse"
          aria-label={collapsed ? "Expand navigation" : "Collapse navigation"}
          aria-expanded={!collapsed}
          onClick={() => {
            const next = !collapsed;
            setCollapsed(next);
            try {
              window.localStorage.setItem(
                navigationStorageKey(principalID),
                JSON.stringify({ collapsed: next }),
              );
            } catch {
              // Navigation remains usable if preferences cannot be persisted.
            }
          }}
        >
          {collapsed ? (
            <ChevronRight size={17} aria-hidden="true" />
          ) : (
            <ChevronLeft size={17} aria-hidden="true" />
          )}
        </button>
      </div>
      <nav aria-label="Primary">
        {groupOrder.map((group) => {
          const items = navigation.filter((item) => item.group === group);
          if (!items.length) return null;
          return (
            <section className="rti-nav-group" key={group}>
              <h2>{groupLabels[group]}</h2>
              {items.map(renderLink)}
            </section>
          );
        })}
        {adminItems.length ? (
          <section className="rti-admin-navigation">
            <button
              type="button"
              className="rti-admin-navigation__trigger"
              aria-label="Admin"
              title={collapsed ? "Admin" : undefined}
              aria-expanded={adminOpen}
              aria-controls="rti-admin-navigation"
              onClick={() => setAdminOpen((current) => !current)}
            >
              <Settings
                className="rti-admin-navigation__compact-icon"
                size={16}
                aria-hidden="true"
              />
              <span>Admin</span>
              <ChevronDown
                className="rti-admin-navigation__chevron"
                size={16}
                aria-hidden="true"
              />
            </button>
            {adminOpen ? (
              <div
                id="rti-admin-navigation"
                className="rti-admin-navigation__content"
              >
                {adminGroupOrder.map((group) => {
                  const items = navigation.filter(
                    (item) => item.group === group,
                  );
                  if (!items.length) return null;
                  return (
                    <section key={group}>
                      <h3>{groupLabels[group]}</h3>
                      {items.map(renderLink)}
                    </section>
                  );
                })}
              </div>
            ) : null}
          </section>
        ) : null}
      </nav>
      <small title={buildLabel}>{buildLabel}</small>
    </aside>
  );
}
