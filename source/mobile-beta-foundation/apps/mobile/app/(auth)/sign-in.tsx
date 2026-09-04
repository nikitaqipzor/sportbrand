import { Link } from "expo-router";
import { Pressable, StyleSheet, Text } from "react-native";

import { useAuth } from "../../src/features/auth/auth-context.tsx";
import { CredentialsForm } from "../../src/features/auth/credentials-form.tsx";

export default function SignInScreen() {
  const { signIn } = useAuth();

  return (
    <CredentialsForm
      testIDPrefix="sign-in"
      kicker="ATHLETICA AI"
      title="С возвращением"
      submitLabel="Войти"
      submit={signIn}
      footer={
        <Link href="/sign-up" asChild>
          <Pressable testID="sign-in-go-to-sign-up" accessibilityRole="link" style={styles.link}>
            <Text style={styles.linkText}>Нет аккаунта? Зарегистрироваться</Text>
          </Pressable>
        </Link>
      }
    />
  );
}

const styles = StyleSheet.create({
  link: { minHeight: 44, alignItems: "center", justifyContent: "center" },
  linkText: { fontWeight: "700", color: "#59615D" }
});
