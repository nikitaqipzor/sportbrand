import { Component, type ReactNode } from "react";
import { Pressable, StyleSheet, Text, View } from "react-native";

import { getCrashReporter } from "./runtime.ts";

type Props = { children: ReactNode; scope: string };
type State = { failed: boolean; message: string };

/**
 * Перехват падения рендера.
 *
 * Без него ошибка в любом экране даёт белый экран и не оставляет следа: у
 * бета-тестера нет ни консоли, ни способа рассказать, что случилось. Здесь
 * падение записывается в журнал и объясняется человеку словами.
 */
export class ErrorBoundary extends Component<Props, State> {
  state: State = { failed: false, message: "" };

  static getDerivedStateFromError(error: unknown): State {
    return { failed: true, message: error instanceof Error ? error.message : String(error) };
  }

  componentDidCatch(error: unknown): void {
    void getCrashReporter().capture(this.props.scope, error);
  }

  render(): ReactNode {
    if (!this.state.failed) return this.props.children;
    return (
      <View testID="crash-screen" style={styles.screen}>
        <Text style={styles.kicker}>ЧТО-ТО СЛОМАЛОСЬ</Text>
        <Text style={styles.title}>Экран не открылся</Text>
        <Text style={styles.muted}>
          Ошибка записана в журнал на устройстве — его видно в «Профиле». Данные
          тренировки не потеряны: всё записанное лежит в очереди.
        </Text>
        <Pressable
          testID="crash-retry"
          accessibilityRole="button"
          style={styles.action}
          onPress={() => this.setState({ failed: false, message: "" })}
        >
          <Text style={styles.actionText}>Попробовать снова</Text>
        </Pressable>
      </View>
    );
  }
}

const styles = StyleSheet.create({
  screen: { flex: 1, padding: 24, gap: 12, justifyContent: "center", backgroundColor: "#FCFBF8" },
  kicker: { fontSize: 12, fontWeight: "700", color: "#C64B2C" },
  title: { fontSize: 28, fontWeight: "800", color: "#151918" },
  muted: { color: "#59615D" },
  action: { minHeight: 52, alignItems: "center", justifyContent: "center", borderRadius: 16, backgroundColor: "#151918" },
  actionText: { color: "#fff", fontWeight: "700" }
});
