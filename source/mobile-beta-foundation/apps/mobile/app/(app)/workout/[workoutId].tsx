import { useLocalSearchParams } from "expo-router";
import { Pressable, StyleSheet, Text, View } from "react-native";

export default function WorkoutScreen() {
  const { workoutId } = useLocalSearchParams<{ workoutId: string }>();
  return <View style={styles.screen}><Text style={styles.kicker}>АКТИВНАЯ ТРЕНИРОВКА · {workoutId}</Text><Text style={styles.title}>Тяга верхнего блока</Text><View style={styles.timer}><Text style={styles.kicker}>ОТДЫХ</Text><Text style={styles.time}>01:18</Text></View><View style={styles.set}><Text>Подход 2 · 62,5 кг · 10 повторов · RIR 2</Text></View><Pressable style={styles.action}><Text style={styles.actionText}>Завершить подход</Text></Pressable></View>;
}
const styles = StyleSheet.create({ screen:{flex:1,padding:24,gap:16,backgroundColor:"#101413"},kicker:{fontSize:12,fontWeight:"700",color:"#AAB3AE"},title:{fontSize:32,fontWeight:"800",color:"#F8F7F3"},timer:{padding:20,borderRadius:24,backgroundColor:"#1A201E"},time:{fontSize:56,fontWeight:"800",color:"#F8F7F3"},set:{padding:20,borderRadius:18,backgroundColor:"#1A201E"},action:{minHeight:56,borderRadius:16,alignItems:"center",justifyContent:"center",backgroundColor:"#C64B2C"},actionText:{fontWeight:"800",color:"#fff"} });
