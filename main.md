如果用户真的配置了 forgejo 平台但没配 ForgejoBaseURL，旧代码会生成一个畸形的 URL /owner/repo
     导致 clone 失败。新代码生成 https://forgejo.example.com/owner/repo 也会失败（example.com
	      不是真实地址），但失败得更明确。
		       不过这里有个问题：如果用户配置了 ForgejoBaseURL，旧代码根本不会走到 forgejo 分支（因为 switch
			        里没有 forgejo case），baseURL 为空，返回 /owner/repo。这意味着即使用户正确配置了
					     ForgejoBaseURL，旧代码也完全忽略了它。所以旧代码对 forgejo 平台实际上是坏的。新代码才是正确的。
