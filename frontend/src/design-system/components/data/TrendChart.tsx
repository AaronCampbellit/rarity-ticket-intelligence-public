export function TrendChart({
  values,
  title,
}: {
  values: number[];
  title: string;
}) {
  const points = values
    .map(
      (value, index) =>
        `${10 + index * (300 / Math.max(1, values.length - 1))},${90 - Math.min(80, Math.max(0, value))}`,
    )
    .join(" ");
  return (
    <svg role="img" aria-label={title} viewBox="0 0 320 100">
      <polyline
        fill="none"
        stroke="currentColor"
        strokeWidth="3"
        points={points}
      />
    </svg>
  );
}
