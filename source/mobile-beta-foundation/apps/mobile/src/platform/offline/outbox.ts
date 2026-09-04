export type OutboxItem<T> = { id: string; userId: string; createdAt: string; payload: T };

export function itemsForUser<T>(items: OutboxItem<T>[], userId: string): OutboxItem<T>[] {
  return items.filter((item) => item.userId === userId);
}

export function purgeForLogout<T>(items: OutboxItem<T>[], userId: string): OutboxItem<T>[] {
  return items.filter((item) => item.userId !== userId);
}

/**
 * Идемпотентная постановка в очередь: повторная запись с тем же id того же
 * пользователя заменяет предыдущую, а не создаёт дубль (см. ADR 0002).
 */
export function enqueue<T>(items: OutboxItem<T>[], item: OutboxItem<T>): OutboxItem<T>[] {
  const withoutDuplicate = items.filter((existing) => !(existing.id === item.id && existing.userId === item.userId));
  return [...withoutDuplicate, item];
}
