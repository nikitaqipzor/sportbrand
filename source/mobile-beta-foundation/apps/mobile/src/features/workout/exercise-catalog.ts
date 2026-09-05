/**
 * Временный встроенный список упражнений.
 *
 * Настоящий справочник (с оборудованием, группами мышц и медиа) готовится
 * отдельно и приедет с сервера. До тех пор экрану тренировки всё равно нужно
 * из чего-то выбирать, поэтому здесь лежит минимум: стабильный идентификатор
 * и русское название. Идентификаторы уже уходят на сервер в подходах и
 * участвуют в clientMutationId, поэтому менять их задним числом нельзя —
 * настоящий справочник обязан сохранить эти же строки.
 *
 * Намеренно ничего, кроме id и названия: методика, техника и нормативы —
 * дело справочника, а не заглушки.
 */
export type CatalogExercise = { id: string; title: string };

export const EXERCISE_CATALOG: readonly CatalogExercise[] = [
  { id: "back-squat", title: "Приседания со штангой" },
  { id: "front-squat", title: "Фронтальные приседания" },
  { id: "deadlift", title: "Становая тяга" },
  { id: "romanian-deadlift", title: "Румынская тяга" },
  { id: "bench-press", title: "Жим лёжа" },
  { id: "incline-bench-press", title: "Жим лёжа на наклонной скамье" },
  { id: "overhead-press", title: "Жим стоя" },
  { id: "barbell-row", title: "Тяга штанги в наклоне" },
  { id: "lat-pulldown", title: "Тяга верхнего блока" },
  { id: "seated-row", title: "Тяга горизонтального блока" },
  { id: "pull-up", title: "Подтягивания" },
  { id: "dip", title: "Отжимания на брусьях" },
  { id: "leg-press", title: "Жим ногами" },
  { id: "leg-curl", title: "Сгибание ног лёжа" },
  { id: "lunge", title: "Выпады" },
  { id: "hip-thrust", title: "Ягодичный мост" },
  { id: "biceps-curl", title: "Подъём на бицепс" },
  { id: "triceps-pushdown", title: "Разгибание на трицепс" },
  { id: "lateral-raise", title: "Махи гантелями в стороны" },
  { id: "plank", title: "Планка" }
] as const;

/** Упражнение по умолчанию для только что начатой тренировки. */
export const DEFAULT_EXERCISE_ID = "back-squat";

const BY_ID = new Map(EXERCISE_CATALOG.map((exercise) => [exercise.id, exercise]));

export const findCatalogExercise = (exerciseId: string): CatalogExercise | null => BY_ID.get(exerciseId) ?? null;

/**
 * Название по идентификатору. Незнакомый id — не ошибка: он мог прийти из
 * снимка, записанного версией с другим списком, и терять из-за этого
 * тренировку нельзя.
 */
export const exerciseTitle = (exerciseId: string): string => BY_ID.get(exerciseId)?.title ?? exerciseId;
