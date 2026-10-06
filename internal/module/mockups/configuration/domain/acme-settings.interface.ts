export interface AcmeSettingsInterface {
    organizationId: string;
    displayName: string;
    supportEmail?: string | null;
    autoSave: boolean;
}
