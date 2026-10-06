import {ModuleDeclarationInterface} from "@liorian/sdk/domain/entities/module.interface";
import {AcmeSettingsApiService} from "./application/service/acme-settings-api-service";

/**
 * Module `CONFIGURATION` (spec module-types §2.1) : module déclaratif de
 * configuration. Ses entrées de réglages s'injectent dans le dropdown du
 * compte connecté (`HeaderTasksConnectedUser`) et dans le hub `/settings` dès
 * que les vérifications du registre passent (V-1 → V-7).
 *
 * Deux contrats coexistent :
 *   - first-party (ce mockup, `external: false`) : le fichier `settings.tsx`
 *     à la racine expose des composants rendus nativement par le socle ;
 *   - tiers : aucune ligne de code — le manifeste déclare `settings.entries`
 *     (routes `/m/<slug>/<path>`) et `declarative.pages`, avec
 *     `entry: index.json`.
 */
const acmeSettingsModule: ModuleDeclarationInterface = {
    identifier: 'config.liorian.acme-settings',
    key: 'ACME_SETTINGS',
    version: '1.0.0',
    name: 'Acme Settings',
    description: 'Module CONFIGURATION d\'exemple : entrées de réglages injectées dans le dropdown du compte connecté et le hub /settings',
    icon: "SettingsIcon",
    logo: undefined,
    service: {
        fetch: AcmeSettingsApiService
    },
    uri: '/acme-settings',
    isEnabled: true,
    isDefault: false,
    type: 'CONFIGURATION',
    external: false,
    category: 'SYSTEM',
    menu: {
        items: [
            {
                label: "Paramètres Acme Settings",
                icon: "SettingsIcon",
                url: '/acme-settings',
            },
        ]
    },
}

export default acmeSettingsModule
