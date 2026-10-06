import {ModuleDeclarationInterface} from "@liorian/sdk/domain/entities/module.interface";

/**
 * Module `WEB_APP_REMOTE` (spec module-types §2.1) : application web hébergée
 * à distance, chargée en iframe sandboxée depuis l'origine enregistrée. Le
 * socle ne scaffold aucune page : l'application vit sur le serveur distant.
 *
 * La section `remote` du manifeste déclare l'origine, les routes autorisées,
 * les backends d'egress et le endpoint de vérification. Avant publication au
 * Store, le serveur distant doit (feature remote-web-app §3) :
 *   1. **Register** — déclarer origine, backends et scopes dans Connect ;
 *   2. **Verify** — prouver la propriété de l'origine (well-known ou DNS TXT) ;
 *   3. **Validate** — CSP, sandbox sans `allow-same-origin`, egress cohérent ;
 *   4. **Approve** — approbation explicite de la modération (jamais
 *      auto-approuvée).
 *
 * Au runtime : données uniquement via `ctx.api` (jeton de module, jamais le
 * jeton de session), egress limité aux backends déclarés, navigation restreinte
 * aux `paths` déclarés. Fail-closed : une vérification retirée bloque le
 * chargement.
 */
const acmeRemoteModule: ModuleDeclarationInterface = {
    identifier: 'mod.liorian.acme-remote',
    key: 'ACME_REMOTE',
    version: '1.0.0',
    name: 'Acme Remote',
    description: 'Module WEB_APP_REMOTE d\'exemple : application hébergée à distance, chargée en iframe sandboxée depuis l\'origine enregistrée',
    icon: "GlobeIcon",
    logo: undefined,
    uri: '/acme-remote',
    isEnabled: true,
    isDefault: false,
    type: 'WEB_APP_REMOTE',
    external: false,
    category: 'SYSTEM',
    menu: {
        items: [
            {
                label: "Acme Remote",
                icon: "GlobeIcon",
                url: '/acme-remote',
            },
        ]
    },
}

export default acmeRemoteModule
