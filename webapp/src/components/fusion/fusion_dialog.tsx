// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import './fusion.css';

import React, {useEffect, useRef} from 'react';
import ReactDOM from 'react-dom';
import {defineMessage, useIntl} from 'react-intl';

import {fusionLayer} from './fusion_ui';
import FusionIcon from './icon';
import {iconTip} from './tip';

export const closeMessage = defineMessage({defaultMessage: 'Close', description: 'The close button (an X) in the corner of a Calls dialog, as its tooltip and its name for screen readers.'});

// The Fusion dialogs open, the last one on top: only that one answers Escape.
const openDialogs: Array<React.MutableRefObject<() => void>> = [];

type DialogProps = {

    // Fusion's modal classes after .am-modal ('am-small' for a message, 'am-src-dialog' for the screen picker...).
    className: string;

    // What names the dialog: the id of its title, or a label when it shows none.
    labelledBy?: string;
    label?: string;
    describedBy?: string;
    role?: 'dialog' | 'alertdialog';
    id?: string;

    // Escape, a click beside the dialog or its close button.
    onClose: () => void;

    // Enter in the dialog (it's a form then).
    onSubmit?: () => void;
    children: React.ReactNode;
};

/**
 * FusionDialog is Calls' dialog under the Fusion UI, drawn as Fusion draws its own: a scrim over the page and an
 * .am-modal in Fusion's layer (#am-layer), whose styles apply to what it contains. Escape or a click beside it
 * closes it, the focus goes to its main button and back to what had it once it's closed. Callers check
 * fusionLayer() first and keep their classic dialog without it.
 */
export function FusionDialog({className, labelledBy, label, describedBy, role = 'dialog', id, onClose, onSubmit, children}: DialogProps) {
    const layer = fusionLayer();
    const modal = useRef<HTMLElement | null>(null);
    const close = useRef(onClose);
    close.current = onClose;

    useEffect(() => {
        const returnFocus = document.activeElement as HTMLElement | null;
        openDialogs.push(close);
        const onKeyDown = (e: KeyboardEvent) => {
            if (e.key === 'Escape' && openDialogs[openDialogs.length - 1] === close) {
                // Only this dialog closes, not the panel or the dialog under it.
                e.preventDefault();
                e.stopPropagation();
                close.current();
            }
        };
        document.addEventListener('keydown', onKeyDown, true);

        const first = modal.current?.querySelector<HTMLElement>('[data-autofocus], .am-btn.am-primary:not(:disabled), .am-btn.am-danger:not(:disabled)') ??
            modal.current?.querySelector<HTMLElement>('button:not(:disabled)');
        first?.focus();

        return () => {
            document.removeEventListener('keydown', onKeyDown, true);
            openDialogs.splice(openDialogs.indexOf(close), 1);
            if (returnFocus?.isConnected) {
                returnFocus.focus?.();
            }
        };
    }, []);

    if (!layer) {
        return null;
    }

    const attrs = {
        id,
        className: 'am-modal ' + className,
        role,
        'aria-modal': true,
        'aria-labelledby': labelledBy,
        'aria-label': labelledBy ? undefined : label,
        'aria-describedby': describedBy,
        'data-calls-dialog': true,
    };

    return ReactDOM.createPortal(
        <div
            className='am-scrim'
            onMouseDown={(e) => {
                if (e.target === e.currentTarget) {
                    onClose();
                }
            }}
        >
            {onSubmit ? (
                <form
                    {...attrs}
                    ref={(el) => {
                        modal.current = el;
                    }}
                    onSubmit={(e) => {
                        e.preventDefault();
                        onSubmit();
                    }}
                >
                    {children}
                </form>
            ) : (
                <div
                    {...attrs}
                    ref={(el) => {
                        modal.current = el;
                    }}
                >
                    {children}
                </div>
            )}
        </div>,
        layer,
    );
}

type PaneProps = {
    titleId: string;
    title: React.ReactNode;
    textId?: string;

    // What the dialog says, under the title.
    children?: React.ReactNode;

    // Its buttons, Fusion's (.am-btn), the main one last.
    actions?: React.ReactNode;

    // A close button in the corner (the default).
    closable?: boolean;
    onClose: () => void;
};

/**
 * FusionDialogPane is what a Fusion message dialog holds: its title, what it says and its buttons, with a close
 * button in the corner, as Fusion's own confirmations.
 */
export function FusionDialogPane({titleId, title, textId, children, actions, closable = true, onClose}: PaneProps) {
    const {formatMessage} = useIntl();
    return (
        <div className='am-pane'>
            {closable && (
                <button
                    type='button'
                    className='am-icon-btn am-close'
                    onClick={onClose}
                    {...iconTip(formatMessage(closeMessage))}
                >
                    <FusionIcon name='x'/>
                </button>
            )}
            <h2 id={titleId}>{title}</h2>
            {children && (
                <div
                    id={textId}
                    className='amcalls-dialog-text'
                >
                    {children}
                </div>
            )}
            {actions && <div className='am-modal-actions'>{actions}</div>}
        </div>
    );
}
