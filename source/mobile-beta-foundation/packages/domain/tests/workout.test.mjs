import test from "node:test";
import assert from "node:assert/strict";

import { validateSet } from "../src/index.ts";

const validSet = {
  workoutId: "workout-1",
  exerciseId: "lat-pulldown",
  setNumber: 2,
  weightKg: 62.5,
  repetitions: 10,
  rir: 2,
  clientMutationId: "6f1a0a4e-1c1f-4a1e-9a3f-0b1f1c2d3e4f"
};

test("валидный подход не даёт замечаний", () => {
  assert.deepEqual(validateSet(validSet), []);
});

test("граничные значения принимаются", () => {
  assert.deepEqual(validateSet({ ...validSet, weightKg: 0, repetitions: 1, rir: 0, setNumber: 1 }), []);
  assert.deepEqual(validateSet({ ...validSet, weightKg: 1000, repetitions: 100, rir: 10 }), []);
});

test("пустые идентификаторы отклоняются", () => {
  for (const field of ["workoutId", "exerciseId", "clientMutationId"]) {
    assert.deepEqual(validateSet({ ...validSet, [field]: "" }), ["required identifier missing"], field);
  }
});

test("номер подхода должен быть целым положительным", () => {
  for (const setNumber of [0, -1, 1.5, Number.NaN]) {
    assert.deepEqual(validateSet({ ...validSet, setNumber }), ["set number must be positive"], String(setNumber));
  }
});

test("вес вне диапазона 0..1000 отклоняется", () => {
  for (const weightKg of [-0.5, 1000.1, Number.NaN, Number.POSITIVE_INFINITY]) {
    assert.deepEqual(
      validateSet({ ...validSet, weightKg }),
      ["weight must be between 0 and 1000 kg"],
      String(weightKg)
    );
  }
});

test("повторы вне диапазона 1..100 отклоняются", () => {
  for (const repetitions of [0, 101, 10.5, Number.NaN]) {
    assert.deepEqual(
      validateSet({ ...validSet, repetitions }),
      ["repetitions must be between 1 and 100"],
      String(repetitions)
    );
  }
});

test("RIR вне диапазона 0..10 отклоняется", () => {
  for (const rir of [-1, 11, 2.5, Number.NaN]) {
    assert.deepEqual(validateSet({ ...validSet, rir }), ["RIR must be between 0 and 10"], String(rir));
  }
});

test("несколько ошибок собираются вместе", () => {
  const issues = validateSet({
    ...validSet,
    workoutId: "",
    setNumber: 0,
    weightKg: -1,
    repetitions: 0,
    rir: 99
  });
  assert.equal(issues.length, 5);
});
