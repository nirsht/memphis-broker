"use strict";(self.webpackChunkmemphis_ui=self.webpackChunkmemphis_ui||[]).push([[6247],{43770:function(e){function s(e){!function(e){e.languages.diff={coord:[/^(?:\*{3}|-{3}|\+{3}).*$/m,/^@@.*@@$/m,/^\d+.*$/m]};var s={"deleted-sign":"-","deleted-arrow":"<","inserted-sign":"+","inserted-arrow":">",unchanged:" ",diff:"!"};Object.keys(s).forEach((function(i){var n=s[i],a=[];/^\w+$/.test(i)||a.push(/\w+/.exec(i)[0]),"diff"===i&&a.push("bold"),e.languages.diff[i]={pattern:RegExp("^(?:["+n+"].*(?:\r\n?|\n|(?![\\s\\S])))+","m"),alias:a}})),Object.defineProperty(e.languages.diff,"PREFIXES",{value:s})}(e)}e.exports=s,s.displayName="diff",s.aliases=[]}}]);