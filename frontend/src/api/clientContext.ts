export const CLIENT_CONTEXT_HEADER = "X-Rarity-Client-ID";

export function clientContextHeaders(clientID: string): Record<string, string> {
  const selected = clientID.trim();
  if (!selected) throw new Error("client_context_required");
  return { [CLIENT_CONTEXT_HEADER]: selected };
}
