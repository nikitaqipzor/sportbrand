export type OutboxItem<T> = { id: string; userId: string; createdAt: string; payload: T };

export function itemsForUser<T>(items: OutboxItem<T>[], userId: string): OutboxItem<T>[] {
  return items.filter((item) => item.userId === userId);
}

export function purgeForLogout<T>(items: OutboxItem<T>[], userId: string): OutboxItem<T>[] {
  return items.filter((item) => item.userId !== userId);
}
