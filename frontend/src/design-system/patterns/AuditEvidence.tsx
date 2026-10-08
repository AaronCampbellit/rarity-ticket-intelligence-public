export type AuditEvidenceEntry = {
  label: string;
  value: string;
};

export function AuditEvidence({ entries }: { entries: AuditEvidenceEntry[] }) {
  return (
    <dl className="rti-audit-evidence">
      {entries.map((entry) => (
        <div key={`${entry.label}-${entry.value}`}>
          <dt>{entry.label}</dt>
          <dd>{entry.value}</dd>
        </div>
      ))}
    </dl>
  );
}
