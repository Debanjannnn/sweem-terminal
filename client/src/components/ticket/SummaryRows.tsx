export function SummaryRows({
  rows,
}: {
  rows: { label: string; value: string | null }[];
}) {
  return (
    <dl className="space-y-2">
      {rows.map((r) => (
        <div key={r.label} className="flex items-center justify-between gap-3 text-sm">
          <dt className="text-muted-foreground">{r.label}</dt>
          <dd className="tnum">{r.value ?? "—"}</dd>
        </div>
      ))}
    </dl>
  );
}
