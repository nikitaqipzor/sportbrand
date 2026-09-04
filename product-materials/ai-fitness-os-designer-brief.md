# AI Fitness OS — Designer Concept Brief

## Задача
Создать 3 визуальных концепта мобильного AI Fitness OS для Android. Текущий UI — функциональный прототип; дизайнер не обязан сохранять его визуальный стиль.

## Главный daily loop
Открыл приложение → увидел состояние сегодня → понял почему → получил тренировку/питание → выполнил → записал → увидел прогресс.

## Обязательные root tabs
1. Сегодня
2. Тренировка
3. Питание
4. Прогресс
5. AI

## Экран 1 — Today
Нужно показать:
- Readiness 0–100;
- 1–3 причины изменения;
- сегодняшнюю тренировку / next action;
- Xiaomi/Health freshness маленьким secondary indicator;
- питание/белок кратко;
- AI Coach prompt.

Не превращать Today в dashboard из 10 одинаковых карточек.

## Экран 2 — Active Workout
Приоритет — управление одной рукой:
- упражнение;
- текущий set;
- вес/reps/RIR;
- rest timer;
- previous performance;
- camera technique action;
- next exercise;
- finish/cancel вторично.

## Экран 3 — Recovery
Показать причинную цепь:
`sleep / energy / stress / soreness / recent load → readiness → workout adaptation`.
Health context (steps/calories/activity) визуально отделять от факторов, которые действительно входят в формулу.

## Экран 4 — Nutrition
Главные сущности:
- kcal/macros remaining;
- быстрый add;
- AI food input/photo;
- meals;
- repeat previous;
- trend/adherence вторично.

## Экран 5 — Progress
Объединить:
- weight/measurements;
- Body Scan;
- strength PR;
- technique CV;
- program adherence.
Не создавать 5 независимых dashboard внутри одного экрана.

## Экран 6 — Connected Devices
Должно быть понятно:
- Xiaomi Watch S3 connected;
- Mi Fitness → Health Connect;
- permissions;
- freshness;
- last sync;
- data coverage;
- privacy/source provenance;
- manual sync как secondary action.

## Экран 7 — AI Coach
AI — не отдельный «магический бот», а conversational interface к данным пользователя.
Желательны context chips: `Сегодня`, `Тренировки`, `Питание`, `Восстановление`, `Прогресс`.
Показывать tool/source trace в compact human form.

## Три направления для концептов

### A. Performance OS
Graphite/black, высокая плотность данных, большие цифры, контрастные графики, ощущение спортивного инструмента.

### B. Calm Health
Светлый нейтральный интерфейс, много воздуха, мягкая визуализация readiness/recovery, минимум визуального давления.

### C. Hybrid Premium — предпочтительный кандидат
Светлая оболочка для дня/аналитики + тёмный focused mode во время Active Workout / Technique Camera. Минимум декоративности, сильная типографика и data visualization.

## Что сохранить из текущего продукта
- Bottom navigation из 5 root sections.
- Readiness как центральная daily сущность.
- Workout adaptation explanation.
- User-visible health provenance/freshness.
- Body/Technique privacy messaging.
- AI как explain/adapt layer, а не source of truth.

## Что можно заменить полностью
- glyph icons;
- нынешнюю систему карточек;
- типографику;
- цвета;
- визуал BodyMap;
- форму input fields;
- layout Home/Progress/Devices;
- style AI chat.

## Accessibility constraints
- touch targets >=48dp;
- color contrast;
- dynamic text friendly;
- не кодировать readiness только цветом;
- error/loading/disabled/offline states обязательны в макетах.
