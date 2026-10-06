import {ModuleDeclarationInterface} from "@liorian/sdk/domain/entities/module.interface";
import {AcmeWidgetsApiService} from "./application/service/acme-widgets-api-service";
import {AcmeWidgetsKpiWidget} from "./presentation/widgets/acme-widgets-kpi.widget";
import {AcmeWidgetsActivityWidget} from "./presentation/widgets/acme-widgets-activity.widget";

/**
 * Module `WIDGET` (spec module-types §2.1) : fournisseur de widgets du
 * tableau de bord. Ses composants sont collectés par `module-registry` et
 * proposés dans le panneau d'ajout de widgets du dashboard quand les
 * vérifications passent (V-1 → V-7).
 *
 * Contrat first-party (feature widget-registration §3) : les composants sont
 * déclarés dans la map `widgets`, avec une clé en pointillés
 * `<module>.<widget>` — la même liste de clés est déclarée dans
 * `manifest.json` (`widgets`).
 *
 * Contrat tiers : descripteurs `declarative.widgets[]` du manifeste, rendus
 * nativement par le socle (aucun code tiers importé).
 */
const acmeWidgetsModule: ModuleDeclarationInterface = {
    identifier: 'widget.liorian.acme-widgets',
    key: 'ACME_WIDGETS',
    version: '1.0.0',
    name: 'Acme Widgets',
    description: 'Module WIDGET d\'exemple : widgets injectés dans la liste du tableau de bord',
    icon: "LayoutDashboardIcon",
    logo: undefined,
    widgets: {
        'acme-widgets.kpi': AcmeWidgetsKpiWidget,
        'acme-widgets.activity': AcmeWidgetsActivityWidget,
    },
    service: {
        fetch: AcmeWidgetsApiService
    },
    uri: '/acme-widgets',
    isEnabled: true,
    isDefault: false,
    type: 'WIDGET',
    external: false,
    category: 'DATA',
    menu: {
        items: []
    },
}

export default acmeWidgetsModule
