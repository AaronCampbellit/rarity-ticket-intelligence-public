import { createContext, useContext, type ReactNode } from "react";

import type { PresentationPreferences } from "./preferences";

const PresentationContext = createContext<PresentationPreferences | null>(null);

export function PresentationProvider({
  value,
  children,
}: {
  value: PresentationPreferences;
  children: ReactNode;
}) {
  return (
    <PresentationContext.Provider value={value}>
      {children}
    </PresentationContext.Provider>
  );
}

export function usePresentation(): PresentationPreferences {
  const value = useContext(PresentationContext);
  if (!value) {
    throw new Error("usePresentation requires PresentationProvider");
  }
  return value;
}

export function useOptionalPresentation(): PresentationPreferences {
  return (
    useContext(PresentationContext) ?? {
      density: "adaptive",
    }
  );
}
