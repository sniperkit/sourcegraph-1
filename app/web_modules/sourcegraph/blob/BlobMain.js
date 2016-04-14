// @flow weak

import React from "react";
import Helmet from "react-helmet";
import last from "lodash/array/last";

import Container from "sourcegraph/Container";
import Dispatcher from "sourcegraph/Dispatcher";
import Blob from "sourcegraph/blob/Blob";
import BlobContentPlaceholder from "sourcegraph/blob/BlobContentPlaceholder";
import BlobToolbar from "sourcegraph/blob/BlobToolbar";
import FileMargin from "sourcegraph/blob/FileMargin";
import DefTooltip from "sourcegraph/def/DefTooltip";
import * as BlobActions from "sourcegraph/blob/BlobActions";
import * as DefActions from "sourcegraph/def/DefActions";
import {routeParams as defRouteParams} from "sourcegraph/def";
import DefStore from "sourcegraph/def/DefStore";
import "sourcegraph/blob/BlobBackend";
import "sourcegraph/def/DefBackend";
import "sourcegraph/build/BuildBackend";
import Style from "sourcegraph/blob/styles/Blob.css";
import {lineCol, lineRange} from "sourcegraph/blob/lineCol";
import urlTo from "sourcegraph/util/urlTo";
import {urlToDef} from "sourcegraph/def/routes";
import {makeRepoRev, trimRepo} from "sourcegraph/repo";
import {httpStatusCode} from "sourcegraph/app/status";
import Header from "sourcegraph/components/Header";

export default class BlobMain extends Container {
	static propTypes = {
		repo: React.PropTypes.string.isRequired,
		rev: React.PropTypes.string,
		path: React.PropTypes.string,
		blob: React.PropTypes.object,
		anns: React.PropTypes.object,
		startLine: React.PropTypes.number,
		startCol: React.PropTypes.number,
		startByte: React.PropTypes.number,
		endLine: React.PropTypes.number,
		endCol: React.PropTypes.number,
		endByte: React.PropTypes.number,
		location: React.PropTypes.object,

		// children are the boxes shown in the blob margin.
		children: React.PropTypes.oneOfType([
			React.PropTypes.arrayOf(React.PropTypes.element),
			React.PropTypes.element,
		]),
	};

	static contextTypes = {
		router: React.PropTypes.object.isRequired,
		status: React.PropTypes.object,
	};

	constructor(props) {
		super(props);
		this._setBlobRef = this._setBlobRef.bind(this);
	}

	componentDidMount() {
		if (super.componentDidMount) super.componentDidMount();
		this._dispatcherToken = Dispatcher.Stores.register(this.__onDispatch.bind(this));
		this._unlistenBefore = this.context.router.listenBefore((location) => {
			// When the route change, if we navigate to a different file clear the
			// currently highlighted def if there is one, otherwise it will be stuck
			// on the next page since no mouseout event can be triggered.
			if (this.state.blob && this.state.highlightedDefObj && !this.state.highlightedDefObj.Error &&
					this.state.blob.Name !== last(this.state.highlightedDefObj.File.split("/"))) {
				Dispatcher.Stores.dispatch(new DefActions.HighlightDef(null));
			}
		});
	}

	componentWillUnmount() {
		if (super.componentWillUnmount) super.componentWillUnmount();
		if (this._unlistenBefore) this._unlistenBefore();
		Dispatcher.Stores.unregister(this._dispatcherToken);
	}

	_unlistenBefore: () => void;
	_dispatcherToken: string;

	reconcileState(state, props) {
		state.repo = props.repo;
		state.rev = props.rev || null;
		state.path = props.path || null;
		state.blob = props.blob || null;
		state.anns = props.anns || null;
		state.startLine = props.startLine || null;
		state.startCol = props.startCol || null;
		state.startByte = props.startByte || null;
		state.endLine = props.endLine || null;
		state.endCol = props.endCol || null;
		state.endByte = props.endByte || null;
		state.children = props.children || null;

		// Def-specific
		state.highlightedDef = DefStore.highlightedDef;
		if (state.highlightedDef) {
			let {repo, rev, def} = defRouteParams(state.highlightedDef);
			state.highlightedDefObj = DefStore.defs.get(repo, rev, def);
		} else {
			state.highlightedDefObj = null;
		}
		state.activeDef = props.defObj && !props.defObj.Error ? urlToDef(props.defObj, state.rev) : null;
		state.startByte = props.defObj && !props.defObj.Error ? props.defObj.DefStart : null;
		state.endByte = props.defObj && !props.defObj.Error ? props.defObj.DefEnd : null;
	}

	onStateTransition(prevState, nextState) {
		if (nextState.highlightedDef && prevState.highlightedDef !== nextState.highlightedDef) {
			let {repo, rev, def} = defRouteParams(nextState.highlightedDef);
			Dispatcher.Backends.dispatch(new DefActions.WantDef(repo, rev, def));
		}
	}

	stores() { return [DefStore]; }

	__onDispatch(action) {
		if (action instanceof BlobActions.SelectLine) {
			this._navigate(action.repo, action.rev, action.path, action.line ? `L${action.line}` : null);
		} else if (action instanceof BlobActions.SelectLineRange) {
			let pos = this._parseHash();
			const startLine = Math.min(pos ? pos.startLine : action.line, action.line);
			const endLine = Math.max(pos ? (pos.endLine || pos.startLine) : action.line, action.line);
			this._navigate(action.repo, action.rev, action.path, startLine && endLine ? `L${lineRange(startLine, endLine)}` : null);
		} else if (action instanceof BlobActions.SelectCharRange) {
			let hash = action.startLine ? `L${lineRange(lineCol(action.startLine, action.startCol), action.endLine && lineCol(action.endLine, action.endCol))}` : null;
			this._navigate(action.repo, action.rev, action.path, hash);
		}
	}

	_navigate(repo, rev, path, hash) {
		let url = urlTo("blob", {splat: [makeRepoRev(repo, rev), path]});

		// Replace the URL if we're just changing the hash. If we're changing
		// more (e.g., from a def URL to a blob URL), then push.
		const replace = this.props.location.pathname === url;
		if (hash) {
			url = `${url}#${hash}`;
		}
		if (replace) this.context.router.replace(url);
		else this.context.router.push(url);
	}

	_setBlobRef(e) {
		if (this.state._blob !== e) {
			this.setState({_blob: e, _getOffsetTopForByte: e ? e.getOffsetTopForByte.bind(e) : null});
		}
	}

	render() {
		if (this.state.blob && this.state.blob.Error) {
			return (
				<Header
					title={`${httpStatusCode(this.state.blob.Error)}`}
					subtitle={`File is not available.`} />
			);
		}

		let pathParts = this.state.path ? this.state.path.split("/") : null;
		return (
			<div className={Style.container}>
				{pathParts && <Helmet title={`${pathParts[pathParts.length - 1]} | ${trimRepo(this.state.repo)}`} />}
				<div className={Style.blobAndToolbar}>
					<BlobToolbar
						repo={this.state.repo}
						rev={this.state.rev}
						path={this.state.path} />
					{(!this.state.blob || !this.state.anns) && <BlobContentPlaceholder />}
					{this.state.blob && !this.state.blob.Error && typeof this.state.blob.ContentsString !== "undefined" && this.state.anns && !this.state.anns.Error &&
					<Blob
						repo={this.state.repo}
						rev={this.state.rev}
						path={this.state.path}
						ref={this._setBlobRef}
						contents={this.state.blob.ContentsString}
						annotations={this.state.anns}
						lineNumbers={true}
						highlightSelectedLines={true}
						highlightedDef={this.state.highlightedDef}
						highlightedDefObj={this.state.highlightedDefObj}
						activeDef={this.state.activeDef}
						startLine={this.state.startLine}
						startCol={this.state.startCol}
						startByte={this.state.startByte}
						endLine={this.state.endLine}
						endCol={this.state.endCol}
						endByte={this.state.endByte}
						scrollToStartLine={true}
						dispatchSelections={true} />}
					{this.state.highlightedDefObj && !this.state.highlightedDefObj.Error && <DefTooltip currentRepo={this.state.repo} def={this.state.highlightedDefObj} />}
				</div>
				<FileMargin getOffsetTopForByte={this.state._getOffsetTopForByte || null} className={Style.margin}>
					{this.props.children}
				</FileMargin>
			</div>
		);
	}
}