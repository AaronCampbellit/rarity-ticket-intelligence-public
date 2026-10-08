export function formatMoney(minor: number, currency: string): string {
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency,
    maximumFractionDigits: 0,
  }).format(minor / 100);
}

export function marginPercent(
  marginMinor: number,
  revenueMinor: number,
): string {
  if (revenueMinor <= 0) return "0%";
  return `${Math.round((marginMinor / revenueMinor) * 100)}%`;
}
