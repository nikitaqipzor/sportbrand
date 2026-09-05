import { describeApiError } from "@athletica/api-client";
import { useRouter } from "expo-router";
import { ActivityIndicator, Pressable, ScrollView, StyleSheet, Text, View } from "react-native";

import { useWorkoutHistory } from "../../src/features/history/use-history.ts";
import { HISTORY_FILTERS, statusLabel } from "../../src/features/history/workout-feed.ts";

function formatDay(iso: string): string {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return "—";
  const day = String(at.getDate()).padStart(2, "0");
  const month = String(at.getMonth() + 1).padStart(2, "0");
  const time = `${String(at.getHours()).padStart(2, "0")}:${String(at.getMinutes()).padStart(2, "0")}`;
  return `${day}.${month}.${at.getFullYear()} · ${time}`;
}

export default function HistoryScreen() {
  const router = useRouter();
  const { items, loading, loadingMore, error, hasMore, empty, filterId, setFilterId, loadMore, reload } =
    useWorkoutHistory();

  return (
    <ScrollView style={styles.screen} contentContainerStyle={styles.content}>
      <Text style={styles.kicker}>ИСТОРИЯ</Text>
      <Text style={styles.title}>Тренировки</Text>

      <ScrollView horizontal showsHorizontalScrollIndicator={false} contentContainerStyle={styles.filters}>
        {HISTORY_FILTERS.map((filter) => {
          const active = filter.id === filterId;
          return (
            <Pressable
              key={filter.id}
              testID={`history-filter-${filter.id}`}
              accessibilityRole="button"
              accessibilityState={{ selected: active }}
              style={[styles.filter, active ? styles.filterActive : null]}
              onPress={() => setFilterId(filter.id)}
            >
              <Text style={[styles.filterText, active ? styles.filterTextActive : null]}>{filter.title}</Text>
            </Pressable>
          );
        })}
      </ScrollView>

      {loading ? <ActivityIndicator testID="history-spinner" /> : null}

      {error ? (
        <View style={styles.card}>
          <Text testID="history-error" style={styles.warn}>
            {describeApiError(error)}
          </Text>
          <Pressable
            testID="history-retry"
            accessibilityRole="button"
            style={styles.secondary}
            onPress={() => void reload()}
          >
            <Text style={styles.secondaryText}>Повторить</Text>
          </Pressable>
        </View>
      ) : null}

      {/* Пустая история — обычное состояние нового аккаунта, а не сбой. */}
      {empty ? (
        <View style={styles.card}>
          <Text testID="history-empty" style={styles.muted}>
            Здесь появятся ваши тренировки. Первую можно начать прямо сейчас.
          </Text>
          <Pressable
            testID="history-start-first"
            accessibilityRole="button"
            style={styles.action}
            onPress={() => router.replace("/")}
          >
            <Text style={styles.actionText}>На главную</Text>
          </Pressable>
        </View>
      ) : null}

      {items.map((workout) => (
        <Pressable
          key={workout.id}
          testID={`history-item-${workout.id}`}
          accessibilityRole="button"
          style={styles.row}
          onPress={() => router.push(`/summary/${workout.id}`)}
        >
          <View style={styles.rowBody}>
            <Text style={styles.rowTitle}>{workout.title || "Без названия"}</Text>
            <Text style={styles.kicker}>
              {formatDay(workout.createdAt)} · {statusLabel(workout.status)}
            </Text>
          </View>
          <Text style={styles.chevron}>›</Text>
        </Pressable>
      ))}

      {/* Следующая страница берётся по курсору сервера, а не по номеру. */}
      {items.length > 0 && hasMore ? (
        <Pressable
          testID="history-load-more"
          accessibilityRole="button"
          accessibilityState={{ disabled: loadingMore, busy: loadingMore }}
          disabled={loadingMore}
          style={styles.secondary}
          onPress={() => void loadMore()}
        >
          <Text style={styles.secondaryText}>{loadingMore ? "Загружаем…" : "Показать ещё"}</Text>
        </Pressable>
      ) : null}

      {items.length > 0 && !hasMore ? (
        <Text testID="history-end" style={styles.kicker}>
          Это все тренировки
        </Text>
      ) : null}

      <Pressable testID="history-home" accessibilityRole="button" style={styles.secondary} onPress={() => router.replace("/")}>
        <Text style={styles.secondaryText}>На главную</Text>
      </Pressable>
    </ScrollView>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, backgroundColor: "#FCFBF8" },
  content: { padding: 24, gap: 12 },
  kicker: { fontSize: 12, fontWeight: "700", color: "#59615D" },
  title: { fontSize: 30, fontWeight: "800", color: "#151918" },
  muted: { color: "#59615D" },
  warn: { fontWeight: "700", color: "#C64B2C" },
  card: { padding: 20, borderRadius: 24, backgroundColor: "#E5ECE9", gap: 12 },
  filters: { gap: 8, paddingVertical: 4 },
  filter: {
    minHeight: 40,
    paddingHorizontal: 14,
    borderRadius: 14,
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "#E5ECE9"
  },
  filterActive: { backgroundColor: "#151918" },
  filterText: { fontWeight: "700", color: "#59615D" },
  filterTextActive: { color: "#fff", fontWeight: "800" },
  row: {
    flexDirection: "row",
    alignItems: "center",
    gap: 12,
    padding: 16,
    borderRadius: 18,
    backgroundColor: "#F1EFE9"
  },
  rowBody: { flex: 1, gap: 2 },
  rowTitle: { fontWeight: "800", color: "#151918" },
  chevron: { fontSize: 24, color: "#59615D" },
  action: { minHeight: 52, alignItems: "center", justifyContent: "center", borderRadius: 16, backgroundColor: "#151918" },
  actionText: { color: "#fff", fontWeight: "700" },
  secondary: { minHeight: 48, alignItems: "center", justifyContent: "center", borderRadius: 16 },
  secondaryText: { fontWeight: "700", color: "#59615D" }
});
