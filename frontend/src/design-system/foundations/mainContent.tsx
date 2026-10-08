import { createContext, useContext, type ReactNode } from "react";

const MainContentIDContext = createContext("main-content");

export function MainContentIDProvider({
  id,
  children,
}: {
  id: string;
  children: ReactNode;
}) {
  return (
    <MainContentIDContext.Provider value={id}>
      {children}
    </MainContentIDContext.Provider>
  );
}

export function useMainContentID() {
  return useContext(MainContentIDContext);
}
