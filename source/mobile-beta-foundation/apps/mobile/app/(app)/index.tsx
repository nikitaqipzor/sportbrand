import { Link } from "expo-router";
import { Pressable, StyleSheet, Text, View } from "react-native";

export default function TodayScreen() {
  return <View style={styles.screen}>
    <Text style={styles.kicker}>ЧЕТВЕРГ · СЕГОДНЯ</Text>
    <Text style={styles.title}>Добрый день, Никита</Text>
    <View style={styles.card}><Text style={styles.kicker}>ГОТОВНОСТЬ</Text><Text style={styles.score}>78</Text><Text>Сон снизил интенсивность на 5%</Text></View>
    <Link href="/workout/demo-strength" asChild><Pressable style={styles.action}><Text style={styles.actionText}>Начать силовую тренировку</Text></Pressable></Link>
  </View>;
}

const styles = StyleSheet.create({
  screen: { flex: 1, padding: 24, gap: 16, backgroundColor: "#FCFBF8" }, kicker: { fontSize: 12, fontWeight: "700", color: "#59615D" },
  title: { fontSize: 32, fontWeight: "800", color: "#151918" }, card: { padding: 20, borderRadius: 24, backgroundColor: "#E5ECE9", gap: 4 },
  score: { fontSize: 56, fontWeight: "800", color: "#151918" }, action: { minHeight: 52, alignItems: "center", justifyContent: "center", borderRadius: 16, backgroundColor: "#151918" }, actionText: { color: "#fff", fontWeight: "700" }
});
