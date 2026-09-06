import { describeApiError } from "@athletica/api-client";
import { useRouter } from "expo-router";
import { useState } from "react";
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, TextInput, View } from "react-native";

import { useExerciseCatalog, useExerciseDictionaries } from "../../src/features/catalog/use-catalog.ts";

export default function LibraryScreen() {
  const router = useRouter();
  const feed = useExerciseCatalog();
  const { dictionaries } = useExerciseDictionaries();
  const [query, setQuery] = useState("");

  const sections = dictionaries.find((entry) => entry.kind === "section")?.items ?? [];
  const activeSection = feed.filters.section;

  return (
    <ScrollView style={styles.screen} contentContainerStyle={styles.content} keyboardShouldPersistTaps="handled">
      <Text style={styles.kicker}>ЭНЦИКЛОПЕДИЯ</Text>
      <Text style={styles.title}>Упражнения</Text>

      <TextInput
        testID="library-search"
        style={styles.search}
        value={query}
        onChangeText={setQuery}
        onSubmitEditing={() => feed.setFilters({ ...feed.filters, q: query.trim() || undefined })}
        placeholder="Поиск по названию"
        returnKeyType="search"
        accessibilityLabel="Поиск упражнения"
      />

      {/* Фильтры строятся из справочников сервера: зашитый список разъехался бы
          с содержимым каталога при первом же импорте. */}
      {sections.length > 0 ? (
        <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.chips}>
          <Pressable
            testID="library-section-all"
            accessibilityRole="button"
            style={[styles.chip, !activeSection && styles.chipActive]}
            onPress={() => feed.setFilters({ ...feed.filters, section: undefined })}
          >
            <Text style={[styles.chipText, !activeSection && styles.chipTextActive]}>Все</Text>
          </Pressable>
          {sections.map((section) => (
            <Pressable
              key={section.code}
              testID={`library-section-${section.code}`}
              accessibilityRole="button"
              style={[styles.chip, activeSection === section.code && styles.chipActive]}
              onPress={() => feed.setFilters({ ...feed.filters, section: section.code })}
            >
              <Text style={[styles.chipText, activeSection === section.code && styles.chipTextActive]}>
                {section.nameRu}
              </Text>
            </Pressable>
          ))}
        </ScrollView>
      ) : null}

      {feed.error ? (
        <View style={styles.card}>
          <Text testID="library-error" style={styles.warn}>
            {describeApiError(feed.error)}
          </Text>
          <Pressable testID="library-retry" accessibilityRole="button" style={styles.secondary} onPress={() => void feed.loadMore()}>
            <Text style={styles.secondaryText}>Повторить</Text>
          </Pressable>
        </View>
      ) : null}

      {feed.loading && feed.items.length === 0 ? <ActivityIndicator testID="library-spinner" /> : null}

      {!feed.loading && feed.items.length === 0 && !feed.error ? (
        <Text testID="library-empty" style={styles.muted}>
          Ничего не нашлось. Каталог наполняется — полная энциклопедия ещё не импортирована.
        </Text>
      ) : null}

      {feed.items.map((exercise) => (
        <Pressable
          key={exercise.id}
          testID={`library-exercise-${exercise.id}`}
          accessibilityRole="button"
          style={styles.row}
          onPress={() => router.push(`/exercise/${exercise.id}`)}
        >
          <View style={styles.rowBody}>
            <Text style={styles.rowTitle}>{exercise.nameRu}</Text>
            <Text style={styles.kicker}>
              {exercise.section}
              {exercise.equipment.length > 0 ? ` · ${exercise.equipment.join(", ")}` : ""}
            </Text>
          </View>
          {/* Уровень показываем, только если он заявлен. null — это не «новичок». */}
          {exercise.difficulty ? <Text style={styles.badge}>{exercise.difficulty}</Text> : null}
        </Pressable>
      ))}

      {feed.exhausted && feed.items.length > 0 ? (
        <Text testID="library-end" style={styles.muted}>
          Это весь каталог
        </Text>
      ) : null}

      {!feed.exhausted && feed.items.length > 0 ? (
        <Pressable testID="library-load-more" accessibilityRole="button" style={styles.action} onPress={() => void feed.loadMore()}>
          <Text style={styles.actionText}>{feed.loading ? "Загружаем…" : "Показать ещё"}</Text>
        </Pressable>
      ) : null}

      <Pressable testID="library-home" accessibilityRole="button" style={styles.secondary} onPress={() => router.replace("/")}>
        <Text style={styles.secondaryText}>На главную</Text>
      </Pressable>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: "#FCFBF8" },
  content: { padding: 24, gap: 10 },
  kicker: { fontSize: 12, fontWeight: "700", color: "#59615D" },
  title: { fontSize: 30, fontWeight: "800", color: "#151918" },
  muted: { color: "#59615D" },
  warn: { fontWeight: "700", color: "#C64B2C" },
  card: { padding: 20, borderRadius: 24, backgroundColor: "#E5ECE9", gap: 8 },
  search: { minHeight: 52, borderRadius: 16, paddingHorizontal: 16, backgroundColor: "#E5ECE9", fontSize: 16, color: "#151918" },
  chips: { gap: 8, paddingVertical: 4 },
  chip: { minHeight: 40, paddingHorizontal: 14, justifyContent: "center", borderRadius: 20, backgroundColor: "#E5ECE9" },
  chipActive: { backgroundColor: "#151918" },
  chipText: { fontWeight: "700", color: "#59615D", fontSize: 13 },
  chipTextActive: { color: "#fff" },
  row: { flexDirection: "row", alignItems: "center", gap: 12, padding: 16, borderRadius: 18, backgroundColor: "#F1EFE9" },
  rowBody: { flex: 1, gap: 2 },
  rowTitle: { fontWeight: "700", color: "#151918" },
  badge: { fontSize: 12, fontWeight: "700", color: "#59615D" },
  action: { minHeight: 52, alignItems: "center", justifyContent: "center", borderRadius: 16, backgroundColor: "#151918" },
  actionText: { color: "#fff", fontWeight: "700" },
  secondary: { minHeight: 48, alignItems: "center", justifyContent: "center", borderRadius: 16 },
  secondaryText: { fontWeight: "700", color: "#59615D" }
});
