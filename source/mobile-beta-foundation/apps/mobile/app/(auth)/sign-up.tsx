import { Link } from "expo-router";
import { Pressable, StyleSheet, Text } from "react-native";

import { useAuth } from "../../src/features/auth/auth-context.tsx";
import { CredentialsForm } from "../../src/features/auth/credentials-form.tsx";
import { PASSWORD_MIN_BYTES } from "../../src/features/auth/validation.ts";

export default function SignUpScreen() {
  const { signUp } = useAuth();

  return (
    <CredentialsForm
      testIDPrefix="sign-up"
      kicker="ATHLETICA AI"
      title="Создать аккаунт"
      submitLabel="Зарегистрироваться"
      passwordHint={`Не короче ${PASSWORD_MIN_BYTES} символов — его придётся вводить редко.`}
      submit={signUp}
      footer={
        <Link href="/sign-in" asChild>
          <Pressable testID="sign-up-go-to-sign-in" accessibilityRole="link" style={styles.link}>
            <Text style={styles.linkText}>Уже есть аккаунт? Войти</Text>
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
