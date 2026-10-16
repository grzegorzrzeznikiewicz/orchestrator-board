import js from "@eslint/js";
import { defineConfig } from "eslint/config";
import tseslint from "typescript-eslint";
import reactHooks from "eslint-plugin-react-hooks";

export default defineConfig(
	{
		ignores: [
			"**/node_modules/**", ".vite/**", ".tanstack/**", "out/**", "dist/**",
			"dist-electron/**", "release/**", "resources/**", "src/docs/**",
			"src/api/schema.ts", "src/renderer/routeTree.gen.ts",
		],
	},
	{
		files: ["**/*.{ts,tsx}"],
		extends: [js.configs.recommended, tseslint.configs.recommended],
		rules: {
			// The compiler already enforces unused locals/parameters. Existing bridge
			// and test boundaries use explicit any; tightening those is separate work.
			"@typescript-eslint/no-unused-vars": "off",
			"@typescript-eslint/no-explicit-any": "off",
		},
	},
	{
		files: ["src/renderer/**/*.{ts,tsx}"],
		plugins: { "react-hooks": reactHooks },
		rules: {
			"react-hooks/rules-of-hooks": "error",
			"react-hooks/exhaustive-deps": "error",
		},
	},
);
