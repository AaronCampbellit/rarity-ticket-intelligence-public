const controlCharacter = /\p{Cc}/u;

export function safeNotificationAction(value: string): string | undefined {
  if (
    value !== value.trim() ||
    value.length > 512 ||
    controlCharacter.test(value)
  ) {
    return undefined;
  }
  if (value.startsWith("#/")) return value;
  if (!value.startsWith("/") || value.startsWith("//")) return undefined;

  try {
    const url = new URL(value, window.location.origin);
    if (
      url.origin !== window.location.origin ||
      url.search !== "" ||
      url.hash !== ""
    ) {
      return undefined;
    }
    return url.pathname;
  } catch {
    return undefined;
  }
}

export function navigateNotificationAction(href: string): boolean {
  const action = safeNotificationAction(href);
  if (!action) return false;
  if (action.startsWith("#/")) {
    window.location.hash = action.slice(1);
  } else {
    window.location.assign(action);
  }
  return true;
}
