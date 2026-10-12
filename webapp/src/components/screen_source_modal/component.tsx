// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import './component.scss';

import {type DesktopCaptureSource} from '@mattermost/desktop-api';
import React from 'react';
import {OverlayTrigger, Tooltip} from 'react-bootstrap';
import {IntlShape} from 'react-intl';
import CompassIcon from 'src/components/icons/compassIcon';
import SpeakerIcon from 'src/components/icons/speaker_icon';
import {logDebug, logErr} from 'src/log';
import {getPlatformInfo, sendDesktopEvent, shareAudioWithScreen, shouldRenderDesktopWidget} from 'src/utils';

interface Props {
    intl: IntlShape,
    show: boolean,
    hideScreenSourceModal: () => void,
}

interface State {
    sources: DesktopCaptureSource[],
    selected: string,
    shareSystemAudio: boolean,
}

export default class ScreenSourceModal extends React.PureComponent<Props, State> {
    private node: React.RefObject<HTMLDivElement>;

    constructor(props: Props) {
        super(props);
        this.node = React.createRef();
        this.state = {
            sources: [],
            selected: '',
            shareSystemAudio: false,
        };
    }

    private keyboardClose = (e: KeyboardEvent) => {
        if (this.props.show && e.key === 'Escape') {
            this.hide();
        }
    };

    private closeOnBlur = (e: Event) => {
        if (!this.props.show) {
            return;
        }
        if (this.node?.current && e.target && this.node.current.contains(e.target as Node)) {
            return;
        }
        this.hide();
    };

    private renderSources = () => {
        return this.state.sources.map((source) => {
            return (
                <button
                    className='style--none screen-source'
                    key={source.id}
                    onClick={() => this.setState({selected: source.id})}
                >
                    <div className={`screen-source-thumbnail ${source.id === this.state.selected ? 'selected' : ''}`}>
                        <img
                            src={source.thumbnailURL}
                            alt=''
                        />
                    </div>

                    <OverlayTrigger
                        placement='bottom'
                        overlay={
                            <Tooltip id='tooltip-screen-source-name'>
                                {source.name}
                            </Tooltip>
                        }
                    >
                        <span className='screen-source-label'>{source.name}</span>
                    </OverlayTrigger>
                </button>
            );
        });
    };

    private hide = () => {
        this.setState({
            sources: [],
            selected: '',
            shareSystemAudio: false,
        });
        this.props.hideScreenSourceModal();
    };

    private shareScreen = () => {
        logDebug('ScreenSourceModal.shareScreen', this.state.selected, shareAudioWithScreen(), this.state.shareSystemAudio);

        const withAudio = shareAudioWithScreen() && this.state.shareSystemAudio;
        if (window.callsClient) {
            // The call runs in this window (the Fusion web UI's calls, or desktop apps without a call window).
            window.callsClient.shareScreen(this.state.selected, withAudio);
        } else if (window.desktopAPI?.shareScreen) {
            logDebug('desktopAPI.shareScreen');
            window.desktopAPI.shareScreen(this.state.selected, withAudio);
        } else if (shouldRenderDesktopWidget()) {
            // DEPRECATED: legacy Desktop API logic (<= 5.6.0)
            sendDesktopEvent('calls-widget-share-screen', {
                sourceID: this.state.selected,
                withAudio,
            });
        }
        this.hide();
    };

    private renderAudioToggle = () => {
        const {formatMessage} = this.props.intl;
        return (
            <>
                <SpeakerIcon
                    style={{
                        width: '16px',
                        height: '16px',
                        fill: 'rgba(var(--center-channel-color-rgb), 0.56)',
                    }}
                />
                {
                    formatMessage({defaultMessage: 'Also share system audio'})}
                <input
                    type='checkbox'
                    name='shareSystemAudio'
                    checked={this.state.shareSystemAudio}
                    onChange={() => this.setState((prevState) => ({shareSystemAudio: !prevState.shareSystemAudio}))}
                />
            </>
        );
    };

    componentDidMount() {
        document.addEventListener('keyup', this.keyboardClose, true);
        document.addEventListener('click', this.closeOnBlur, true);

        if (!window.desktopAPI?.getDesktopSources) {
            // DEPRECATED: legacy Desktop API logic (<= 5.6.0)
            window.addEventListener('message', this.handleDesktopEvents);
        }
    }

    componentWillUnmount() {
        document.removeEventListener('keyup', this.keyboardClose, true);
        document.removeEventListener('click', this.closeOnBlur, true);

        if (!window.desktopAPI?.getDesktopSources) {
            // DEPRECATED: legacy Desktop API logic (<= 5.6.0)
            window.removeEventListener('message', this.handleDesktopEvents);
        }
    }

    componentDidUpdate(prevProps: Props) {
        if (!prevProps.show && this.props.show) {
            const payload = {
                types: ['window', 'screen'] as Array<'screen' | 'window'>,
                thumbnailSize: {
                    width: 400,
                    height: 400,
                },
            };

            if (window.desktopAPI?.getDesktopSources) {
                logDebug('desktopAPI.getDesktopSources');
                window.desktopAPI.getDesktopSources(payload).then((sources) => {
                    if (sources.length === 0) {
                        logErr('desktopAPI.getDesktopSources returned empty');
                        this.props.hideScreenSourceModal();
                        return;
                    }

                    // Exclude the calls widget window from the list.
                    sources = sources.filter((source) => source.name !== 'Calls Widget');

                    this.setState({
                        sources,
                        selected: sources[0]?.id || '',
                    });
                }).catch((err) => {
                    logErr('desktopAPI.getDesktopSources failed', err);
                    this.props.hideScreenSourceModal();
                });
            } else {
                // Send a message to the desktop app to get the sources needed
                // DEPRECATED: legacy Desktop API logic (<= 5.6.0)
                sendDesktopEvent('get-desktop-sources', payload);
            }
        }
    }

    // DEPRECATED: legacy Desktop API logic (<= 5.6.0)
    handleDesktopEvents = (ev: MessageEvent) => {
        if (ev.origin !== window.origin) {
            return;
        }

        if (ev.data.type === 'desktop-sources-result') {
            const sources = ev.data.message;
            this.setState({
                sources,
                selected: sources[0]?.id || '',
            });
        } else if (ev.data.type === 'calls-error' && ev.data.message.err === 'screen-permissions') {
            this.props.hideScreenSourceModal();
        }
    };

    render() {
        const {formatMessage} = this.props.intl;

        if (!this.props.show || this.state.sources.length === 0) {
            return null;
        }

        const platformName = getPlatformInfo();

        // System audio sharing is supported on Linux and Windows platforms only.
        const shouldRenderAudioToggle = shareAudioWithScreen() && (platformName === 'Linux' || platformName === 'Windows');

        return (
            <div className='screen-source-modal-backdrop'>
                <div
                    id='calls-screen-source-modal'
                    className='screen-source-modal'
                    ref={this.node}
                >
                    <div className='screen-source-modal-header'>
                        <span className='screen-source-modal-title'>
                            {formatMessage({defaultMessage: 'Choose what to share'})}
                        </span>
                        <button
                            className='style--none screen-source-modal-close'
                            onClick={this.hide}
                        >
                            <CompassIcon icon='close'/>
                        </button>
                    </div>
                    <div className='screen-source-modal-body'>
                        {this.renderSources()}
                    </div>

                    {shouldRenderAudioToggle &&
                        <label className='screen-source-modal-audio'>
                            {this.renderAudioToggle()}
                        </label>
                    }

                    <div className='screen-source-modal-footer'>
                        <button
                            className='style--none screen-source-modal-cancel'
                            onClick={this.hide}
                        >{formatMessage({defaultMessage: 'Cancel'})}</button>
                        <button
                            className='style--none screen-source-modal-join'
                            onClick={this.shareScreen}
                        >{formatMessage({defaultMessage: 'Share'})}</button>
                    </div>
                </div>
            </div>
        );
    }
}
