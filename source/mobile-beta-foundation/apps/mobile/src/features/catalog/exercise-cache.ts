import type { ExerciseSummary } from "@athletica/api-client";

/**
 * Локальная копия каталога упражнений.
 *
 * Упражнение выбирают в зале, где связи может не быть вовсе. Если бы выбор
 * ходил в сеть, добавить упражнение посреди офлайн-тренировки было бы нельзя —
 * а это ровно тот сценарий, ради которого построена вся офлайн-очередь.
 * Поэтому каталог читается с диска, а сеть только обновляет копию.
 */
export type CachedExercise = { id: string; title: string; section: string; equipment: string };

export type ExerciseCache = {
  list: (query?: string) => Promise<CachedExercise[]>;
  put: (exercises: ExerciseSummary[]) => Promise<void>;
  count: () => Promise<number>;
};

export const toCached = (exercise: ExerciseSummary): CachedExercise => ({
  id: exercise.id,
  title: exercise.nameRu,
  section: exercise.section,
  equipment: exercise.equipment.join(", ")
});

/** Поиск по подстроке без учёта регистра — та же семантика, что у сервера. */
export function matches(exercise: CachedExercise, query: string): boolean {
  const needle = query.trim().toLocaleLowerCase("ru");
  if (needle === "") return true;
  return exercise.title.toLocaleLowerCase("ru").includes(needle);
}

export type ExerciseCacheMemoryDb = { rows: CachedExercise[] };

export const createExerciseCacheMemoryDb = (): ExerciseCacheMemoryDb => ({ rows: [] });

export function createMemoryExerciseCache(db: ExerciseCacheMemoryDb = createExerciseCacheMemoryDb()): ExerciseCache {
  return {
    list: async (query = "") => db.rows.filter((row) => matches(row, query)).sort((a, b) => a.title.localeCompare(b.title, "ru")),
    put: async (exercises) => {
      for (const exercise of exercises) {
        const cached = toCached(exercise);
        const index = db.rows.findIndex((row) => row.id === cached.id);
        // Обновление, а не вставка второй строки: идентификатор неизменяем.
        if (index >= 0) db.rows[index] = cached;
        else db.rows.push(cached);
      }
    },
    count: async () => db.rows.length
  };
}
