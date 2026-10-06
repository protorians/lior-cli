import {ModuleDeclarationInterface} from "@liorian/sdk/domain/entities/module.interface";

/**
 * Module `THEME` (spec module-types §2.1) : fournisseur de thèmes. Ses
 * palettes de tokens s'ajoutent à la liste des thèmes (`/settings/themes`)
 * quand les vérifications du registre passent (V-1 → V-7).
 *
 * Un thème est **uniquement un jeu de tokens** — jamais de code. La palette
 * est déclarée dans `manifest.json` (`themes[]`) : `scheme` absent vaut
 * `light`, le bloc `dark` est optionnel. Les clés de `tokens` sont validées
 * contre la whitelist `MODULE_THEME_TOKENS` du SDK.
 *
 * Un module `THEME` first-party peut en plus fournir une palette CSS compilée
 * (`styles/`) ajoutée à la source de vérité `packages/sdk/src/themes.css` et
 * exposée via le registre `SOCLE_THEMES`.
 */
const acmeThemeModule: ModuleDeclarationInterface = {
    identifier: 'theme.liorian.acme-theme',
    key: 'ACME_THEME',
    version: '1.0.0',
    name: 'Acme Theme',
    description: 'Module THEME d\'exemple : palettes de tokens injectées dans la liste des thèmes',
    icon: "PaletteIcon",
    logo: undefined,
    uri: '/acme-theme',
    isEnabled: true,
    isDefault: false,
    type: 'THEME',
    external: false,
    category: 'SYSTEM',
    menu: {
        items: []
    },
}

export default acmeThemeModule
