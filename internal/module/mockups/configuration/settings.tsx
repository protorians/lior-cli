"use client"

import type * as React from "react";
import {AcmeSettingsForm} from "./presentation/settings/acme-settings-form";

/**
 * Contrat `settings.tsx` d'un module `CONFIGURATION` compilé dans le socle
 * (feature configuration-settings §3).
 *
 * Le socle ne connaît rien de ces composants : il rend le tableau exporté par
 * défaut sous les paramètres communs du module, dans le hub `/settings`, et
 * alimente le dropdown du compte connecté avec une entrée par module.
 *
 * L'interface canonique (`ModuleSettingsEntryInterface`) vit dans le socle
 * (`@/modules/settings/domain/module-settings.interface.ts`) : les entrées
 * ci-dessous s'y conforment structurellement — `{label, description?,
 * component}` — sans import socle, pour rester compilables hors monorepo.
 */
const settings = [
    {
        label: "Préférences Acme Settings",
        description: "Préférences du module Acme Settings : identité affichée et e-mail de support",
        component: AcmeSettingsForm,
    },
] satisfies { label: string; description?: string; component: React.FC }[];

export default settings;
