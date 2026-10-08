export type NotificationItem = {
  id: string;
  title: string;
  body: string;
  actionPath: string;
  contentClassification: string;
  createdAt: string;
  readAt?: string;
  version: number;
};

export type NotificationPage = {
  notifications: NotificationItem[];
  nextCursor?: string;
};

export type NotificationCenterAPI = {
  list(
    cursor?: string,
    limit?: number,
    signal?: AbortSignal,
  ): Promise<NotificationPage>;
  unreadCount(signal?: AbortSignal): Promise<number>;
  markRead(
    id: string,
    expectedVersion: number,
    signal?: AbortSignal,
  ): Promise<NotificationItem>;
};

export type NotificationCenterProps = {
  api?: NotificationCenterAPI;
  onNavigate?: (href: string) => void;
};
