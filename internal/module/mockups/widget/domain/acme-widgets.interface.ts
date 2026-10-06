export interface AcmeWidgetsAnalyticsInterface {
    total: number;
    inProgress: number;
    completed: number;
    archived: number;
}

export interface AcmeWidgetsActivityInterface {
    id: string;
    label: string;
    author?: string | null;
    at?: string | null;
}
