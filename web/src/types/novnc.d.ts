declare module '@novnc/novnc' {
  export interface RFBCredentials {
    username?: string;
    password?: string;
    target?: string;
  }

  export interface RFBOptions {
    credentials?: RFBCredentials;
    shared?: boolean;
    repeaterID?: string;
    wsProtocols?: string | string[];
  }

  export default class RFB extends EventTarget {
    constructor(target: HTMLElement, url: string, options?: RFBOptions);

    scaleViewport: boolean;
    resizeSession: boolean;
    background: string;

    disconnect(): void;
    sendCredentials(credentials: RFBCredentials): void;
  }
}
