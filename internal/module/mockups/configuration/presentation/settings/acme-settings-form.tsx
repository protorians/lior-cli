"use client"

import * as React from "react";
import {Button} from "@liorian/sdk/presentation/ui/button";
import {LegacyInput} from "@liorian/sdk/presentation/ui/legacy-input";
import {FieldGroup} from "@liorian/sdk/presentation/ui/field";
import {toast} from "sonner";
import {SaveIcon} from "lucide-react";
import {AcmeSettingsApiService} from "../../application/service/acme-settings-api-service";
import {AcmeSettingsInterface} from "../../domain/acme-settings.interface";

/**
 * Composant de paramètres rendu par le hub `/settings` via `settings.tsx`.
 *
 * Le composant vit dans le bundle du socle (module first-party) : il peut
 * importer le SDK et appeler l'API du module par `ModuleApiService`. Un module
 * installé (tiers) ne peut pas fournir ce composant — il déclare une route de
 * paramètres dans `manifest.settings.entries`, rendue dans son iframe.
 */
export function AcmeSettingsForm() {
    const [displayName, setDisplayName] = React.useState("");
    const [supportEmail, setSupportEmail] = React.useState("");
    const [saving, setSaving] = React.useState(false);

    React.useEffect(() => {
        AcmeSettingsApiService.get()
            .then((response) => {
                const settings = (response.data as any)?.data;
                if (!settings) return;
                setDisplayName(settings.displayName ?? "");
                setSupportEmail(settings.supportEmail ?? "");
            })
            .catch(() => {
                // Premier lancement : le module n'a encore aucun réglage persisté.
            });
    }, []);

    const save = async () => {
        setSaving(true);
        try {
            await AcmeSettingsApiService.update({
                displayName: displayName.trim(),
                supportEmail: supportEmail.trim() || null,
            });
            toast.success("Paramètres enregistrés");
        } catch {
            toast.error("Erreur lors de l'enregistrement des paramètres");
        } finally {
            setSaving(false);
        }
    };

    return (
        <FieldGroup className="gap-4 max-w-lg">
            <LegacyInput
                id="acme-settings-display-name"
                label="Nom affiché"
                description="Nom du module visible dans les écrans de paramétrage"
                input={{
                    type: "text",
                    placeholder: "Acme Settings",
                    value: displayName,
                    onChange: (e) => setDisplayName(e.target.value),
                }}
            />
            <LegacyInput
                id="acme-settings-support-email"
                label="E-mail de support"
                input={{
                    type: "email",
                    placeholder: "support@acme.example",
                    value: supportEmail,
                    onChange: (e) => setSupportEmail(e.target.value),
                }}
            />
            <div>
                <Button onClick={save} disabled={saving}>
                    <SaveIcon className="size-4"/>
                    Enregistrer
                </Button>
            </div>
        </FieldGroup>
    );
}
