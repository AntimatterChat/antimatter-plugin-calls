// Copyright (c) 2020-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import styled from 'styled-components';

export const leftCol = 'col-sm-4';
export const rightCol = 'col-sm-8';

export const LabelRow = styled.div`
    display: flex;
`;

export const Label = styled.span`
    margin-right: 8px;
`;

export const LeftBox = styled.div`
    display: flex;
    flex-direction: column;
    padding: 24px;
    max-width: 584px;
    background: var(--center-channel-bg);
    box-shadow: 0 2px 3px rgba(0, 0, 0, 0.08);
    border-radius: 4px;
`;

export const RadioInput = styled.input<{disabled: boolean}>`
&& {
  display: grid;
  width: 1.6rem;
  height: 1.6rem;
  border: 1px solid rgba(var(--center-channel-color-rgb),0.24);
  border-radius: 50%;
  color: rgba(var(--center-channel-color-rgb),0.24);
  appearance: none;
  place-content: center;
  cursor: pointer;
  background-color: white;
  margin: 0;
  opacity: ${({disabled}) => (disabled ? 0.5 : 1.0)};
  cursor: ${({disabled}) => (disabled ? 'not-allowed' : 'pointer')};

  &:checked {
    border-color: var(--denim-button-bg);
  }

  &::before {
    width: 8px;
    height: 8px;
    background: var(--denim-button-bg);
    content: "";
    border-radius: 50%;
    transform: scale(0);
    transform-origin: center center;
    transition: 200ms transform ease-in-out;
  }

  &:checked::before {
    transform: scale(1);
  }
}
`;

export const RadioInputLabel = styled.label<{$disabled: boolean}>`
  display: inline-flex;
  margin-top: 8px;
  margin-right: 24px;
  width: fit-content;
  flex-direction: row;
  align-items: center;
  margin-bottom: 0;
  cursor: pointer;
  font-size: 14px;
  font-weight: 400;
  gap: 8px;
  line-height: 20px;

  opacity: ${({$disabled}) => ($disabled ? 0.5 : 1.0)};
  cursor: ${({$disabled}) => ($disabled ? 'not-allowed' : 'pointer')};
`;

export const SectionTitle = styled.div`
  font-family: 'Metropolis', sans-serif;
  font-size: 16px;
  font-weight: 600;
  line-height: 24px;
  display: flex;
  align-items: center;
  gap: 8px;
`;

export const UnavailableSubtitle = styled.div`
  color: var(--center-channel-color-72);
  font-family: "Open Sans";
  font-size: 14px;
  font-style: italic;
  font-weight: 400;
  line-height: 20px;
`;

export const TextInput = styled.input<{disabled: boolean}>`
&& {
  cursor: ${({disabled}) => (disabled ? 'not-allowed' : 'auto')};
}
`;
