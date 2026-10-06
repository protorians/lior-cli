import {SystemConsoleLevel} from "./enums/system-console-level.enum";

export interface SystemConsoleDiagnosticInterface {
    id: string;
    label: string;
    detail?: string | null;
    level: SystemConsoleLevel;
    at?: string | null;
}

export interface SystemConsoleHealthInterface {
    status: 'HEALTHY' | 'DEGRADED' | 'DOWN';
    checkedAt?: string | null;
    diagnostics: SystemConsoleDiagnosticInterface[];
}
