import type { ReactNode } from "react";

import { RarityBrand } from "./RarityBrand";

export function TopBar({
  title,
  children,
}: {
  title: string;
  children: ReactNode;
}) {
  return (
    <header className="rarity-topbar">
      <div className="rarity-topbar__context">
        <div className="rarity-topbar__mobile-brand">
          <RarityBrand variant="compact" />
        </div>
        <span className="rarity-topbar__title">{title}</span>
      </div>
      <div className="rarity-topbar__actions">{children}</div>
    </header>
  );
}
