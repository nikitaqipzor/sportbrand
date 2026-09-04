// Metro для pnpm-монорепо: следим за всем воркспейсом и добавляем корневой
// node_modules, чтобы резолвились символические ссылки на @athletica/*.
// Иерархический поиск НЕ отключаем: pnpm хранит транзитивные зависимости
// во вложенных node_modules внутри .pnpm, и без него сборка падает.
const path = require("node:path");

const { getDefaultConfig } = require("expo/metro-config");

const projectRoot = __dirname;
const workspaceRoot = path.resolve(projectRoot, "..", "..");

const config = getDefaultConfig(projectRoot);

config.watchFolders = [workspaceRoot];
config.resolver.nodeModulesPaths = [
  path.resolve(projectRoot, "node_modules"),
  path.resolve(workspaceRoot, "node_modules")
];
config.resolver.unstable_enableSymlinks = true;
config.resolver.unstable_enablePackageExports = true;

module.exports = config;
