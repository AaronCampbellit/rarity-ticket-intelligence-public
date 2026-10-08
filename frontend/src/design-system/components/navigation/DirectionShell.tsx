import type { ReactNode } from "react";

import { GroupedSidebar, type NavigationItem } from "./GroupedSidebar";

type DirectionShellProps = {
  brand: ReactNode;
  navigation: NavigationItem[];
  activeID: string;
  buildLabel: string;
  principalID: string;
  open: boolean;
  onNavigate: () => void;
};

export function DirectionShell(props: DirectionShellProps) {
  return <GroupedSidebar {...props} />;
}
