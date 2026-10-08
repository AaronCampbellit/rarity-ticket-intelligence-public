import type { ReactNode } from "react";

export function VersionedEditor({
  version,
  label,
  children,
}: {
  version: number;
  label: string;
  children: ReactNode;
}) {
  return (
    <section className="rti-versioned-editor" aria-label={`${label} editor`}>
      <p className="rti-versioned-editor__version">
        {label} version {version}
      </p>
      {children}
    </section>
  );
}
