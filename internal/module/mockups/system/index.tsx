import {ModuleDeclarationInterface} from "@liorian/sdk/domain/entities/module.interface";
import {SystemConsoleApiService} from "./application/service/system-console-api-service";

/**
 * Module `SYSTEM` (spec module-types §2.1) : module `WEB_APP_LOCAL` first-party
 * pouvant demander des accès administrateurs. Disponible uniquement sous Tauri
 * (`manifest.platforms.web.supported: false` — sur web, l'état est
 * `platform_unsupported` et le module n'est pas monté) et masqué aux
 * développeurs tiers.
 *
 * Les privilèges demandés sont déclarés dans `manifest.admin` (`roles`,
 * `scopes`) : ils ne sont accordés au runtime que si le module est first-party,
 * le runtime est Tauri, le rôle de l'utilisateur est listé et les scopes sont
 * accordés — fail-closed (feature system-modules §3.2).
 */
const systemConsoleModule: ModuleDeclarationInterface = {
    identifier: 'system.liorian.system-console',
    key: 'SYSTEM_CONSOLE',
    version: '1.0.0',
    name: 'System Console',
    description: 'Module SYSTEM d\'exemple : application d\'administration first-party, Tauri uniquement, avec accès administrateur',
    icon: "TerminalIcon",
    logo: undefined,
    service: {
        fetch: SystemConsoleApiService
    },
    uri: '/system-console',
    isEnabled: true,
    isDefault: false,
    type: 'SYSTEM',
    external: false,
    category: 'ADMINISTRATION',
    menu: {
        items: [
            {
                label: "Console système",
                icon: "TerminalIcon",
                url: '/system-console',
            },
        ]
    },
}

export default systemConsoleModule
