/**
 * Тренировки, которые начаты на устройстве и ещё не подтверждены сервером.
 *
 * Зачем отдельно от снимка активной тренировки: снимок стирается при
 * завершении, а неотправленные подходы остаются в очереди. Без реестра
 * название тренировки, проведённой целиком офлайн, потерялось бы к моменту
 * синхронизации, и на сервере она появилась бы безымянной.
 */
export type WorkoutRegistryEntry = { workoutId: string; title: string; created: boolean };

export type WorkoutRegistry = {
  remember: (userId: string, workoutId: string, title: string) => Promise<void>;
  get: (userId: string, workoutId: string) => Promise<WorkoutRegistryEntry | null>;
  /** Сервер подтвердил тренировку — повторно создавать её не нужно. */
  markCreated: (userId: string, workoutId: string) => Promise<void>;
  forget: (userId: string, workoutId: string) => Promise<void>;
  purgeUser: (userId: string) => Promise<void>;
};

export type WorkoutRegistryMemoryDb = { rows: (WorkoutRegistryEntry & { userId: string })[] };

export const createWorkoutRegistryMemoryDb = (): WorkoutRegistryMemoryDb => ({ rows: [] });

export function createMemoryWorkoutRegistry(
  db: WorkoutRegistryMemoryDb = createWorkoutRegistryMemoryDb()
): WorkoutRegistry {
  const find = (userId: string, workoutId: string) =>
    db.rows.find((row) => row.userId === userId && row.workoutId === workoutId);

  return {
    remember: async (userId, workoutId, title) => {
      const existing = find(userId, workoutId);
      // Повторный старт не переписывает подтверждённую запись: название
      // тренировки принадлежит серверу с момента создания.
      if (existing) return;
      db.rows.push({ userId, workoutId, title, created: false });
    },
    get: async (userId, workoutId) => {
      const row = find(userId, workoutId);
      return row ? { workoutId: row.workoutId, title: row.title, created: row.created } : null;
    },
    markCreated: async (userId, workoutId) => {
      const row = find(userId, workoutId);
      if (row) row.created = true;
    },
    forget: async (userId, workoutId) => {
      db.rows = db.rows.filter((row) => !(row.userId === userId && row.workoutId === workoutId));
    },
    purgeUser: async (userId) => {
      db.rows = db.rows.filter((row) => row.userId !== userId);
    }
  };
}
