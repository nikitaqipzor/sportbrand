import { useState, type ReactNode } from "react";
import { ActivityIndicator, Pressable, StyleSheet, Text, TextInput, View } from "react-native";

import { useAuth } from "./auth-context.tsx";
import { authErrorMessage, fieldErrorsFrom } from "./messages.ts";
import { hasErrors, validateCredentials, type FieldErrors } from "./validation.ts";

export type CredentialsFormProps = {
  /** Префикс testID: все интерактивные элементы формы получают свой. */
  testIDPrefix: string;
  kicker: string;
  title: string;
  submitLabel: string;
  passwordHint?: string;
  submit: (credentials: { email: string; password: string }) => Promise<boolean>;
  footer: ReactNode;
};

export function CredentialsForm({
  testIDPrefix,
  kicker,
  title,
  submitLabel,
  passwordHint,
  submit,
  footer
}: CredentialsFormProps): ReactNode {
  const { pending, error, clearError } = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [revealed, setRevealed] = useState(false);
  const [touched, setTouched] = useState<FieldErrors>({});

  // Ошибки формы: сначала клиентские, поверх — то, что вернул сервер в 422.
  const serverFields = error ? fieldErrorsFrom(error) : {};
  const fields: FieldErrors = { ...serverFields, ...touched };

  async function onSubmit() {
    const found = validateCredentials({ email, password });
    setTouched(found);
    if (hasErrors(found)) return;
    clearError();
    const ok = await submit({ email: email.trim(), password });
    if (!ok) setTouched({});
  }

  return (
    <View style={styles.screen}>
      <Text style={styles.kicker}>{kicker}</Text>
      <Text style={styles.title}>{title}</Text>

      <View style={styles.field}>
        <Text style={styles.label}>Почта</Text>
        <TextInput
          testID={`${testIDPrefix}-email-input`}
          accessibilityLabel="Адрес электронной почты"
          style={[styles.input, fields.email ? styles.inputInvalid : null]}
          value={email}
          onChangeText={(value) => {
            setEmail(value);
            setTouched((prev) => ({ ...prev, email: undefined }));
          }}
          placeholder="athlete@example.com"
          placeholderTextColor="#8A938E"
          autoCapitalize="none"
          autoCorrect={false}
          keyboardType="email-address"
          textContentType="emailAddress"
          editable={!pending}
        />
        {fields.email ? (
          <Text testID={`${testIDPrefix}-email-error`} style={styles.fieldError}>
            {fields.email}
          </Text>
        ) : null}
      </View>

      <View style={styles.field}>
        <Text style={styles.label}>Пароль</Text>
        <TextInput
          testID={`${testIDPrefix}-password-input`}
          accessibilityLabel="Пароль"
          style={[styles.input, fields.password ? styles.inputInvalid : null]}
          value={password}
          onChangeText={(value) => {
            setPassword(value);
            setTouched((prev) => ({ ...prev, password: undefined }));
          }}
          placeholder="Не короче 10 символов"
          placeholderTextColor="#8A938E"
          secureTextEntry={!revealed}
          autoCapitalize="none"
          autoCorrect={false}
          editable={!pending}
        />
        <Pressable
          testID={`${testIDPrefix}-password-visibility`}
          accessibilityRole="button"
          accessibilityLabel={revealed ? "Скрыть пароль" : "Показать пароль"}
          style={styles.reveal}
          onPress={() => setRevealed((prev) => !prev)}
        >
          <Text style={styles.revealText}>{revealed ? "Скрыть пароль" : "Показать пароль"}</Text>
        </Pressable>
        {fields.password ? (
          <Text testID={`${testIDPrefix}-password-error`} style={styles.fieldError}>
            {fields.password}
          </Text>
        ) : passwordHint ? (
          <Text style={styles.hint}>{passwordHint}</Text>
        ) : null}
      </View>

      {error ? (
        <Text testID={`${testIDPrefix}-form-error`} style={styles.formError}>
          {authErrorMessage(error)}
        </Text>
      ) : null}

      <Pressable
        testID={`${testIDPrefix}-submit`}
        accessibilityRole="button"
        accessibilityState={{ disabled: pending, busy: pending }}
        disabled={pending}
        style={[styles.action, pending ? styles.actionPending : null]}
        onPress={() => {
          void onSubmit();
        }}
      >
        {pending ? (
          <ActivityIndicator testID={`${testIDPrefix}-spinner`} color="#fff" />
        ) : (
          <Text style={styles.actionText}>{submitLabel}</Text>
        )}
      </Pressable>

      {footer}
    </View>
  );
}

const styles = StyleSheet.create({
  screen: { flex: 1, padding: 24, gap: 16, backgroundColor: "#FCFBF8", justifyContent: "center" },
  kicker: { fontSize: 12, fontWeight: "700", color: "#59615D" },
  title: { fontSize: 32, fontWeight: "800", color: "#151918" },
  field: { gap: 6 },
  label: { fontSize: 13, fontWeight: "700", color: "#59615D" },
  input: {
    minHeight: 52,
    borderRadius: 16,
    paddingHorizontal: 16,
    backgroundColor: "#E5ECE9",
    color: "#151918",
    fontSize: 16
  },
  inputInvalid: { borderWidth: 2, borderColor: "#C64B2C" },
  reveal: { alignSelf: "flex-start", minHeight: 32, justifyContent: "center" },
  revealText: { fontSize: 13, fontWeight: "700", color: "#59615D" },
  hint: { fontSize: 12, color: "#59615D" },
  fieldError: { fontSize: 13, fontWeight: "700", color: "#C64B2C" },
  formError: { fontSize: 14, fontWeight: "700", color: "#C64B2C" },
  action: {
    minHeight: 52,
    alignItems: "center",
    justifyContent: "center",
    borderRadius: 16,
    backgroundColor: "#151918"
  },
  actionPending: { backgroundColor: "#3A403D" },
  actionText: { color: "#fff", fontWeight: "700" }
});
