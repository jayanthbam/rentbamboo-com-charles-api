package training

// TrainingExample represents a single training example with email text and response flag
type TrainingExample struct {
	Email         string
	NeedsResponse bool
}

// GetTrainingExamples returns the collection of training examples
func GetTrainingExamples() []TrainingExample {
	return []TrainingExample{
		// Multifamily apartment inquiries
		{"Hi, I saw your listing for the 2-bedroom apartment on Main Street. Is it still available for June 1st? Thanks, Sarah", true},
		{"Hello, I'm interested in the studio apartment listed on Apartments.com. What utilities are included in the $1200 rent? -Michael", true},
		{"Good afternoon, do you have any 1BR units available in your downtown complex? Looking to move next month. Best, Jessica", true},
		{"I'm inquiring about the 3-bedroom unit in Oakwood Apartments. Is parking included or is that an additional fee? Thanks, Robert", true},
		{"Are pets allowed in your River Heights apartments? I have a well-behaved 30lb dog. Thanks! - Emma", true},
		{"Hi there, what's the square footage of the 2BR/2BA apartment on your website? Also, is the washer/dryer in-unit or shared? Thanks, Daniel", true},
		{"Hello, I'm relocating to the area and interested in your luxury apartments. Do you offer any short-term leases or is it strictly 12 months? Regards, Lauren", true},
		{"What floor is the available unit on in Metropolitan Towers? And does the building have an elevator? Thx, Chris", true},
		{"Hi, is the security deposit equal to one month's rent for the Parkview apartments? Also, what's your pet policy? Thanks, Jennifer", true},
		{"I saw your listing for Sunset Apartments. Are utilities included in the rent? And what's the application fee? - Carlos", true},

		// Single-family home inquiries
		{"Hello, I'm interested in renting the 3BR house on Elm Street. Is there a backyard and is it fenced? Thanks, Andrew", true},
		{"Hi there, we're a family of four looking at your rental home on Oak Drive. How's the school district in that area? Best, Melissa", true},
		{"I'm interested in the single-family home on Pine Lane. Does it have central air conditioning? Thanks, Jason", true},
		{"Hello! Is the 4-bedroom house on Washington Ave still available? And would you consider a 2-year lease? -Rebecca", true},
		{"I saw your listing for the ranch house. Is lawn maintenance included or is the tenant responsible? Thanks, Mark", true},

		// Amenity-specific inquiries
		{"Does your Riverside Apartments complex have a fitness center? If so, what equipment is available? Thanks, Alicia", true},
		{"Hi, I'm wondering if the Mountain View Apartments have in-unit laundry or if there's a laundry facility on-site? Thanks, Kevin", true},
		{"Is there a pool in your Sunnydale complex? And if so, when is it open during the year? Thanks! -Natalie", true},
		{"Hello, does the Cherry Hill property have high-speed internet already set up or would I need to arrange that myself? Best, Brandon", true},
		{"Are the balconies at Lakeview Apartments large enough for patio furniture? Thanks, Victoria", true},

		// Pet-related inquiries
		{"Hi there, what's your pet policy for the downtown lofts? I have two cats. Thanks, Patrick", true},
		{"Hello! Do you charge pet rent in addition to a pet deposit at Westside Apartments? I have a small dog. -Stephanie", true},
		{"I'm interested in your Forest Glen property. Are there breed or weight restrictions for dogs? Thanks, Eric", true},
		{"Do your Highland Apartments have any designated pet areas or dog parks nearby? Thanks! -Brittany", true},
		{"Hello, I have a service animal. Would that be subject to your pet fees at Parkside Towers? Thanks, David", true},

		// Parking inquiries
		{"Is covered parking available at your Lincoln Ave apartments? And is there an additional cost? Thanks, Michelle", true},
		{"Hi there, how many parking spaces come with the 2BR unit at The Reserve? We have two cars. -Ryan", true},
		{"Hello, is street parking readily available near the Maple Street apartment, or do you offer off-street options? Thanks, Rachel", true},
		{"What's the parking situation at Urban Lofts? Is there a garage or lot? Thanks, Greg", true},
		{"Hi, is visitor parking available at the Willow Creek Apartments? My parents visit frequently. -Amanda", true},

		// Application process inquiries
		{"Hello, what's required to apply for the Garden District apartment? And is there an application fee? Best regards, Jonathan", true},
		{"Hi, how long does your application approval process usually take for Hillside Terrace? Thanks, Nicole", true},
		{"What's your policy on credit checks for the downtown units? My score is good but not perfect. -Brad", true},
		{"Hello, do you require first and last month's rent plus security deposit for the Oakwood property? Thanks, Megan", true},
		{"I'm interested in the apartment on 5th Street. What's the minimum income requirement to qualify? -Tyler", true},

		// Move-in related inquiries
		{"Is the move-in date flexible for the Harbor View apartment? I could move May 25th or June 1st. Thanks, Samantha", true},
		{"Hi there, is the Valencia property available for immediate move-in? I need a place within 2 weeks. -Jose", true},
		{"Hello, do you prorate the first month's rent if I move in mid-month at Lakeshore Apartments? Thanks, Amber", true},
		{"What floor is the available unit at Tower Plaza? And is there a freight elevator for moving furniture? -Derek", true},
		{"Hi, are there any move-in specials currently for your downtown apartments? Thanks, Chelsea", true},

		// Lease terms inquiries
		{"Hello, what's the minimum lease term for the Hillcrest property? Would you consider 9 months? Thanks, Bryan", true},
		{"I'm inquiring about your flexible lease options at Riverview. Do you offer month-to-month after the initial term? -Laura", true},
		{"Hi there, what's your policy on breaking a lease if I need to relocate for work at The Aspens? Thanks, Josh", true},
		{"Are lease renewals typically at the same rate or should I expect an increase at Parkview Apartments? -Heather", true},
		{"Hello, do you offer any discounts for longer lease terms at your Broadway location? Thanks, Mike", true},

		// Miscellaneous inquiries
		{"Hi, is the unit at Woodridge Apartments cable-ready? And is there high-speed internet in the building? Thanks, Tiffany", true},
		{"Hello, how's the cell phone reception in the Valley Creek area? I work from home and need reliable service. -Alex", true},
		{"Are utilities included in the rent for the Lakeshore property? If not, what's the average monthly cost? Thanks, Lisa", true},
		{"Hi there, how's the noise level at City Center Apartments? I'm a light sleeper. Best, Tony", true},
		{"Does the Willow Grove property have air conditioning? Is it central air or window units? Thanks, Emily", true},

		// More varied inquiries
		{"Hello, I'm a graduate student looking for housing near the university. Is your Oak Street property within walking distance? Thanks, Jordan", true},
		{"Hi, does your Highland Park complex offer furnished options? I'm relocating temporarily for work. -Maria", true},
		{"Are there any grocery stores within walking distance of the Sunset property? Thanks, Nathan", true},
		{"Hello, do the units at Riverdale have dishwashers? That's a must-have for me. Thanks, Kim", true},
		{"Hi there, what's the average age range of tenants at The Metropolitan? I'm in my late 20s. -Devin", true},
		{"Is there guest parking available at the Cedar Ridge apartments? I have family that visits regularly. Thanks, Monica", true},
		{"Hello! Is the Madison Ave property close to public transportation? I don't own a car. Best, Trevor", true},
		{"Hi, I'm interested in your eco-friendly apartments. What energy-efficient features does the building have? -Julia", true},
		{"Are there storage units available for rent at Parkview Towers? I have some seasonal items. Thanks, Eric", true},
		{"Hello, how's the water pressure in the Westside apartments? My current place is terrible. -Shannon", true},

		// Community amenities inquiries
		{"Hi, does your Silver Lake complex have a community room residents can reserve for events? Thanks, Vanessa", true},
		{"Hello, is there an on-site maintenance team at The Residences? How quickly do they typically respond? -Paul", true},
		{"I'm interested in the rooftop deck mentioned in your Skyline listing. Is it furnished and when is it accessible? Thanks, Ashley", true},
		{"Hi there, does the Brookside community have a business center with printers/scanners? I work remotely. -Keith", true},
		{"Hello! Is there a doorman or security personnel at the entrance of the Grandview building? Thanks, Cynthia", true},

		// Roommate and occupancy inquiries
		{"Hi, what's your policy on roommates for the 2BR on Jackson Street? We're not related but want to share. Thanks, Zach", true},
		{"Hello, what's the maximum occupancy for the 3-bedroom house on Maple Avenue? We're a family of 5. -Leslie", true},
		{"I'm inquiring about your co-signing policy for the university apartments. I'm a student with limited income. Thanks, Brooke", true},
		{"Hi there, do you allow two unrelated individuals to rent the 2BR unit at Lakeside? We're friends looking to share. -Miguel", true},
		{"What's your policy on overnight guests at the downtown studio apartments? My girlfriend visits on weekends. Thanks, Ben", true},

		// Location-specific inquiries
		{"Hello, how far is the Parkside property from the nearest hospital? I work night shifts there. Thanks, Kayla", true},
		{"Hi, is the Highland View apartment in a quiet neighborhood? I need minimal street noise. -Jeremy", true},
		{"I'm interested in your downtown loft. How close is it to the financial district? I'd prefer to walk to work. Thanks, Erica", true},
		{"Hello! Is the Crestview property in a good school district? We have an elementary-aged child. Best, Scott", true},
		{"Hi there, are there any parks or walking trails near the Willowbrook apartments? I jog daily. -Tracy", true},

		// More pet-specific inquiries
		{"Hello, do the Oakmont units have any grassy areas for dogs, or would I need to walk to a nearby park? Thanks, Curtis", true},
		{"Hi, I have a 50lb lab mix. Do your weight restrictions at Parkview allow for this size? -Dana", true},
		{"I have an emotional support animal. What documentation do you require for the Riverside apartments? Thanks, Marcus", true},
		{"Hello! Does your Creekside property have any dog washing stations? My retriever loves mud. -Angela", true},
		{"Hi there, are there many other dog owners in the Westview complex? Looking for a pet-friendly community. Thanks, Ian", true},

		// Security and safety inquiries
		{"Hello, does the Mountain Creek property have secure entry and surveillance cameras? Safety is important to me. Thanks, Sharon", true},
		{"Hi, what type of locks are on the apartment doors at City Lights? Are there deadbolts? -Vincent", true},
		{"I'm interested in the safety features of your downtown units. Is there a security guard or buzzer system? Thanks, Kendra", true},
		{"Hello! Does the Parkside building have fire sprinklers and smoke detectors in each unit? -Diego", true},
		{"Hi there, what's the crime rate like in the neighborhood around Oakwood Apartments? Thanks, Olivia", true},

		// Fitness and recreation inquiries
		{"Hello, what are the hours of operation for the gym at Lakeview Towers? I workout early mornings. Thanks, Brett", true},
		{"Hi, does the pool at Sunridge have lap lanes or is it just a lounge pool? I swim for exercise. -Andrea", true},
		{"I saw your Highland property has a fitness center. What equipment does it have? Looking for free weights. Thanks, Jared", true},
		{"Hello! Are there tennis courts at the Pinebrook community? That's a hobby of mine. -Veronica", true},
		{"Hi there, does your Creekside property have any walking paths or jogging trails? Thanks, Doug", true},

		// More diverse inquiry styles and topics
		{"Hello, I'm a remote worker - is there high-speed internet available at Meadowbrook, and do the units have good spaces for home offices? Thanks, Felicia", true},
		{"Hi! My band practices weekly - are the walls at Sunset Apartments well insulated for sound? Don't want to disturb neighbors. -Matt", true},
		{"I'm interested in the eco-friendly features mentioned at Greenview Apartments. Is the building LEED certified? Thanks, Sophia", true},
		{"Hello, is there recycling available at The Madison building? Environmental practices are important to me. -Gabriel", true},
		{"Hi there, does the Riverpoint property have any electric vehicle charging stations? I drive a Tesla. Thanks, Naomi", true},
		{"I'm a chef and cooking is important - what kind of kitchen appliances come with the downtown lofts? Gas or electric stove? -Ray", true},
		{"Hello! I have allergies - are the floors in the Westlake units carpet or hardwood? Carpet is difficult for me. Thanks, Leah", true},
		{"Hi, does your Hillcrest property have smart home features like programmable thermostats or keyless entry? -Colin", true},
		{"Are the windows in the Skyview apartments double-paned? Concerned about heating efficiency in winter. Thanks, Bianca", true},
		{"Hello, is there adequate natural lighting in the 1BR units at The Franklin? I work from home and prefer natural light. -Darren", true},

		// Additional specific amenity inquiries
		{"Hi there, do the units at The Wellington have bathtubs or just stand-up showers? I prefer a tub. Thanks, Caitlin", true},
		{"Hello, are the kitchens at Lakeside equipped with garbage disposals? That's a feature I use often. -Hector", true},
		{"I saw your Pinecrest listing - do the units have ceiling fans? I prefer those to A/C sometimes. Thanks, Faith", true},
		{"Hi, what type of countertops are in the Riverdale kitchens? Looking for something durable. -Warren", true},
		{"Hello! Do the balconies at Skyline Towers have outlets? I like to work outside sometimes. Thanks, Nina", true},

		// Longer, more detailed inquiries
		{"Good morning, I recently toured your 2-bedroom unit at Oakwood Commons and am very interested. Before I submit an application, could you clarify a few things? First, is the monthly parking fee of $50 per car or per apartment? Second, you mentioned flexible move-in dates - would the second week of July work? Finally, is renters insurance required? Thank you for your help! Best regards, Timothy Wallace", true},
		{"Hello! My husband and I are relocating to the area for work and your Riverpoint property caught our attention. We have a few questions: 1) Are utilities included in the rent? 2) Is there reserved covered parking? 3) Do you allow two cats (both fixed and declawed)? 4) Is there storage space available beyond what's in the unit? We're planning to visit next weekend and would love to tour if possible. Thanks so much! - Eleanor & James", true},
		{"Hi there, I'm a medical resident at University Hospital looking for a place close to work. Your Central Avenue apartments seem perfect location-wise. I work odd hours including night shifts, so I'm particularly interested in information about soundproofing between units and blackout blinds/curtains. Also, is there a secure package receiving system for when I'm at work during deliveries? Finally, given my 80+ hour work weeks, is there any flexibility on the income verification requirements? Thank you for your consideration. Sincerely, Dr. Sarah Chen", true},

		// More casual, brief inquiries
		{"hey is the 1br still available? how much is rent again? thx -jay", true},
		{"Interested in the downtown apt. When can I see it? Tom", true},
		{"do u accept pets? have 2 cats. Liz", true},
		{"apartment still for rent? call me 555-1234 please", true},
		{"whats the sq footage of the 2br? and parking situation?", true},

		// Very specific apartment features
		{"Hello, I work from home and need reliable internet. Does your Parkside property have fiber optic internet available, or what providers service the building? Also, is there a dedicated space that would work as a home office in the 2BR floor plan? Thanks, Diana", true},
		{"Hi there, I'm a light sleeper. Are the Westview apartments located away from traffic noise, and do they have soundproofing between units? Also, what floor is the available unit on? I prefer higher floors. Thanks, Peter", true},
		{"Hello! Do the units at Lakeshore have full-size washers and dryers or apartment-sized ones? Also, is there a limit to how many people can be at the pool at once? Thanks, Kristen", true},
		{"I need excellent cell reception for work. What carriers get good service in your Highland building? Also, are there restrictions on satellite dish installation for international channels? -Omar", true},
		{"Hi, I cook a lot. Do the kitchens at The Madison have gas or electric stoves? And is there a pantry for food storage? Thanks, Rosa", true},

		// Accessibility inquiries
		{"Hello, I have mobility issues and use a walker. Is the Cedar Creek property ADA compliant? Are there any steps to enter the building or unit? Thanks, Barbara", true},
		{"Hi there, I'm deaf and rely on visual alerts. Can the doorbell/fire alarms at Parkview be set up with flashing lights? Thanks, Richard", true},
		{"I use a wheelchair - are the doorways at The Pines wide enough for accessibility? Is the bathroom equipped with grab bars? -Teresa", true},
		{"Hello! My father has limited mobility. Does your first-floor unit at Willow Creek have a walk-in shower rather than a tub? Thanks, Phillip", true},
		{"Hi, are the kitchen counters at a height that would be accessible from a wheelchair at your Glendale property? -Lydia", true},

		// Relocation inquiries
		{"Hello from Chicago! I'm relocating for a job next month and interested in your downtown properties. Since I can't visit in person before moving, do you offer virtual tours? Also, can the application and lease signing be completed remotely? Thanks! -Adriana", true},
		{"Hi there, I'll be moving from overseas in August for graduate school. What documentation would I need as an international renter for your university apartments? I don't have a US credit history yet. Thanks, Raj", true},
		{"Hello! I'm being transferred by my company to your area. They'll be paying my rent directly - do you have experience with corporate housing arrangements? Also, I'll need to furnish quickly - are there rental furniture options nearby? Thanks, Christine", true},

		// Additional pet inquiries
		{"Hello, I have an aquarium with tropical fish. Is there any policy about fish tanks at Parkside Towers? Mine is 50 gallons. -Edwin", true},
		{"Hi there, do your Westwood units have any nearby walking trails for dogs? Also, are there breed restrictions beyond the typical 'aggressive breeds'? I have a Great Dane. Thanks, Wendy", true},
		{"I foster rescue dogs occasionally before they find permanent homes. Would this be allowed at your Cedar property if I stay within your pet limit at any given time? -Martin", true},

		// Unique situations
		{"Hello, I'm a musician and need to practice my violin daily. Are there any noise restriction hours at Parkview, or would my playing likely cause issues with neighbors? Thanks, Vivian", true},
		{"Hi there, I run a small online business shipping handmade items. Would I be able to receive supply deliveries and ship packages from the Oakridge property? It's all small-scale. -Calvin", true},
		{"Hello! I sometimes work night shifts as a nurse. Is the Creekside complex generally quiet during daytime hours so I could sleep? Thanks, Hannah", true},
		{"I practice yoga daily and need some floor space. What's the largest open area in the living room of your 1BR units at The Meadows? -Leon", true},
		{"Hi, I collect vintage motorcycles (non-running, as art). Could I keep one in my apartment at Industrial Lofts, or is there secure storage available? Thanks, Suzanne", true},

		// More multifamily property inquiries
		{"Good morning, do the units at Riverside have individual thermostats or is the temperature controlled building-wide? I prefer to set my own temperature. Thanks, Gavin", true},
		{"Hello, I noticed your listing for Grove Park has balconies. What are the dimensions? I'd like to know if my patio furniture would fit. -Carly", true},
		{"Hi there, are the windows at The Summit apartments energy efficient? My current place has terrible drafts in winter. Thanks, Hugh", true},
		{"I'm interested in natural light. Which direction do the windows face in the available unit at Lakeview? -Pam", true},
		{"Hello! How thick are the walls between units at City Center? I'm concerned about noise from neighbors. Thanks, Leo", true},

		// More lease and policy inquiries
		{"Hi, what's your policy on subletting at The Grand if I need to travel for an extended period? Thanks, Tanya", true},
		{"Hello, do you require tenants to have liability insurance at Parkview Commons? If so, what minimum coverage? -Frank", true},
		{"I may need to add a roommate mid-lease. What's the process for that at Westside Apartments? Thanks, Jenny", true},
		{"Hi there, what's your late rent payment policy at Hillcrest? Is there a grace period? -Dominic", true},
		{"Hello! Do you run background checks as part of the application process for Arlington Heights? Thanks, Gloria", true},

		// Technology and connectivity inquiries
		{"Hi, I work in tech and need reliable internet. Is there fiber optic service at The Montgomery? What's the typical download speed? Thanks, Alan", true},
		{"Hello, do the units at The Edison have smart thermostats or other smart home features? -Bridget", true},
		{"I'm a gamer and need low-latency internet. What ISPs are available at your downtown property, and are there any bandwidth caps? Thanks, Nate", true},
		{"Hi there, do you allow installation of EV charging equipment at Fox Run if I pay for the installation? I drive an electric car. -Diana", true},
		{"Hello! Is there good cell reception for Verizon inside the Highpoint building? My current apartment is like a dead zone. Thanks, Marco", true},

		// More single-family home inquiries
		{"Good afternoon, I'm interested in the house on Maple Drive. Is the attic finished or suitable for storage? Thanks, Lorraine", true},
		{"Hi, does the rental home on Oak Street have a sprinkler system for the lawn? And who's responsible for lawn care? -Victor", true},
		{"Hello, I noticed the house on Pine Lane has a basement. Is it finished, and has there ever been water damage or flooding? Thanks, Paula", true},
		{"I'm interested in the single-family home on Elmwood Ave. How old is the roof and HVAC system? -Gordon", true},
		{"Hi there, does the backyard at the Washington Street house have any fruit trees or garden spaces? I enjoy gardening. Thanks, Irene", true},

		// Very specific community amenities
		{"Hello, I use Amazon frequently. Does your Northpoint property have Amazon lockers or a secure package receiving system? Thanks, Clark", true},
		{"Hi, I'm a cyclist. Does Riverview Apartments have bike storage, and are there good bike paths nearby? -Paige", true},
		{"Hello! Does the community room at The Wellington have a kitchen that residents can use when hosting gatherings? Thanks, Lucas", true},
		{"I enjoy grilling. Does Lakeside Apartments have community BBQ areas, or are personal grills allowed on balconies? -Fiona", true},
		{"Hi there, does your Pinehill complex have a car washing station or area where residents can wash their vehicles? Thanks, Eddie", true},

		// More security inquiries
		{"Hello, what type of entry system does The Belmont use? Key fob, keypad code, or traditional keys? Thanks, Nancy", true},
		{"Hi, are there security cameras in the parking garage at City View Apartments? My car was broken into at my last place. -Oscar", true},
		{"I work late hours. How well-lit are the parking areas and walkways at night at Meadowbrook? Thanks, Renee", true},
		{"Hello! Is there an on-site security guard at The Chancellor, or is it more of a secured-access building? -Wayne", true},
		{"Hi there, does Forest Glen have any neighborhood watch program or security patrols? Safety is my top priority. Thanks, Tammy", true},

		// Appliance and fixture inquiries
		{"Hello, what brand/model of refrigerator is in the Parkside units? I need something with a good-sized freezer. Thanks, Gina", true},
		{"Hi, do the bathrooms at The Ashton have ventilation fans? My current place doesn't and it's a problem. -Perry", true},
		{"Hello! What kind of water heater do the Lakewood units have? Is it tank or tankless? I need consistent hot water. Thanks, Jamal", true},
		{"Do the units at Brookside have ceiling lights in the bedrooms or would I need to bring lamps? -Melanie", true},
		{"Hi there, what type of blinds/window coverings come with the Summit apartments? Thanks, Kurt", true},

		// Additional professional/lifestyle inquiries
		{"Hello, I'm a professional pianist and need to practice regularly. Are there any units at The Wellington with extra soundproofing, or perhaps end units with fewer shared walls? Thank you, Clara", true},
		{"Hi, I'm a chef and cooking is my passion. Do the kitchens at The Palms have gas stoves and good ventilation? Also, is there room for some of my specialty appliances? Thanks, Anthony", true},
		{"Hello! I work rotating hospital shifts and sleep at odd hours. Are there blackout blinds in the bedrooms at Parkview, and how is the daytime noise level? Thanks, Elena", true},
		{"Hi there, I run an online crafting business (very quiet, no machinery). Would operating this from my apartment at The Madison violate any lease terms? -Brian", true},
		{"I'm an artist looking for good natural light. Do the north-facing units at Studio Lofts get decent daylight for painting? Thanks, Isaac", true},

		// Final batch of varied inquiries - full conversations
		{"I'm looking at The Pines apartments and would love more details about entertainment setup options. Currently I have a pretty extensive home theater system with surround sound that I'd like to bring with me. Would I be allowed to install this in one of your units? Let me know what modifications you allow. - Xavier\n\nI'd be happy to help outline our guidelines for audio system installations. What specific components are you looking to set up? That way I can give you accurate information about what's permitted. - Jake\n\nThanks for the quick response! I have a 7.1 surround setup that needs in-wall speaker wiring and mounting brackets. Would definitely have it professionally installed. Is that something you'd consider? - Xavier", true},

		{"Just touring rentals in the area and had a question about the unit at Cedarwood. Due to chemical sensitivities, I need to know if there's been any recent painting or new carpeting installed? Even minor fumes can be a problem. - Yvonne\n\nThanks for asking about this. The unit was actually painted about 3 months ago with new carpet installed around the same time. Given your sensitivities, would you prefer to look at units without recent updates? I have several that might work better. - Jake\n\nYes, that would be much better. What units do you have available that haven't had recent renovations? - Yvonne", true},

		{"Hi - I work remotely and reliable internet is crucial. Have there been any connectivity issues at The Monroe recently? I'm on video calls with clients all day and can't risk dropouts. What's been the experience of other remote workers there? - Zachary\n\nI understand how important stable internet is. Let me check with some of our current remote working residents about their experiences. - Jake\n\nGreat, looking forward to hearing what you find out. Consistent connection is my top priority. - Zachary", true},

		{"Question about The Arlington's 2BR units - my daughter plays cello for youth orchestra and needs practice space. Would the layout work for a musician? Main concerns are enough room for the instrument and noise levels for neighbors. - Abby\n\nI think our layout could work well for a musician. Could you tell me more about when she typically practices and how much space she needs? That would help me recommend the best unit. - Jake\n\nShe usually practices 1-2 hours after school, between 4-6pm. Needs space for herself, the cello, and a music stand. - Abby", true},

		{"Curious about The Jefferson's green initiatives. Do you have composting available or just standard recycling? Environmental practices are a big factor in my housing choice. - Benjamin\n\nWe have comprehensive recycling currently. What specific sustainable features are you looking for? That would help me outline our relevant programs. - Jake\n\nMainly interested in properties making real efforts toward sustainability. What green initiatives do you have in place? - Benjamin", true},

		{"Have some questions about air quality at Parkside Towers. I deal with bad seasonal allergies so HVAC filtration is really important. What kind of air filters do you use and how often are they changed? - Caroline\n\nI understand your air quality concerns. Let me get specifics about our filtration system and maintenance schedule. - Jake\n\nJust confirmed we use MERV-13 filters which are changed regularly. - Jake", true},

		{"Looking at Lakeview and need to check the bathroom electrical setup. As a hairstylist who works from home, I need GFCI outlets near the mirrors since I use multiple styling tools. - Damien\n\nLet me verify the exact electrical specifications for you. - Jake\n\nGood news - all bathroom outlets are GFCI-protected. - Jake", true},

		// More multifamily inquiries - full conversations
		{"I'd like to know more about noise levels at The Summit. My current apartment has terrible soundproofing and I can hear everything. - Quinn\n\nI understand your concern about noise. Could you tell me what type of noise bothers you most? That will help me address specific sound mitigation features. - Jake\n\nMainly footsteps from above and voices through walls. Those are the worst offenders. - Quinn", true},

		{"Looking at your downtown properties and wondering about visitor policies. My family visits frequently and I'd like to understand overnight guest rules. - Rita\n\nHappy to explain our guest policies. How many visitors do you typically have and how long do they usually stay? - Jake\n\nUsually my parents, about once a month for 2-3 days. Sometimes my sister too. - Rita", true},

		{"Question about The Madison - curious if you have any units with home office potential? Need a dedicated workspace. - Samuel\n\nAbsolutely! Could you share what your ideal workspace setup would look like? That would help me recommend specific floor plans. - Jake\n\nNeed room for two monitors, desk, and some storage. Prefer natural light for video calls. - Samuel", true},

		{"Are there any co-working spaces near Parkside Towers? I work hybrid and like having options. - Tara\n\nThere are several nearby options. What's your ideal walking distance to a co-working space? - Jake\n\nPrefer within 10-15 minutes walk if possible. Also interested in coffee shops nearby. - Tara", true},

		{"Interested in The Oaks but concerned about package security. Had issues with theft at my current place. - Ulysses\n\nWe take package security seriously. What types of deliveries do you typically receive? That will help me explain relevant features. - Jake\n\nMostly Amazon, some meal deliveries, occasional valuables. - Ulysses", true},

		{"Looking at Riverside - how's the natural light in the units? Big factor for my plants. - Valerie\n\nHappy to discuss lighting. What types of plants do you have? That will help me suggest optimal unit orientations. - Jake\n\nMostly tropical plants that need bright indirect light. Some succulents too. - Valerie", true},

		{"Question about The Metropolitan's amenities - specifically interested in the gym. - Wesley\n\nI can detail our fitness center. What equipment do you typically use in your workouts? - Jake\n\nMainly free weights and need a squat rack. Some cardio equipment too. - Wesley", true},

		{"Considering The Pines but need to verify internet options. Work in tech and need reliable connection. - Xena\n\nUnderstand the importance of good internet. What speeds do you typically need for your work? - Jake\n\nNeed at least 100Mbps down/up, fiber preferred if available. - Xena", true},

		{"Your Hillside property looks promising but wondering about the neighborhood. - Yuri\n\nHappy to share details about the area. What aspects of the neighborhood are most important to you? - Jake\n\nMainly safety, walkability, and nearby restaurants/shops. - Yuri", true},

		{"Question about The Heights - any restrictions on decorating? Like to make my space personal. - Zara\n\nWe support personalizing your space within guidelines. What types of decorating did you have in mind? - Jake\n\nWant to paint walls, hang artwork, maybe some floating shelves. - Zara", true},

		{"Looking at Parkview and curious about utility costs. My current place is expensive to heat/cool. - Adrian\n\nHappy to discuss typical utility costs. What do you currently pay, and which utilities concern you most? - Jake\n\nPaying about $200/month now, mostly worried about heating/cooling efficiency. - Adrian", true},

		{"The Monarch looks nice but need to check cell reception. Work requires reliable phone service. - Blair\n\nI can check carrier coverage. Which provider do you use? - Jake\n\nVerizon is my carrier, need solid indoor reception. - Blair", true},

		{"Interested in Cedar Ridge but have questions about move-in costs and fees. - Cameron\n\nHappy to break down all costs. What's your planned move-in timeframe? - Jake\n\nLooking at next month, need to understand deposits and pet fees especially. - Cameron", true},

		{"The Downtown Lofts caught my eye. How's the nightlife noise level? - Dakota\n\nThe area does have an active nightlife. What's your typical sleep schedule? - Jake\n\nUsually in bed by 10pm, light sleeper especially on weekends. - Dakota", true},

		{"Question about Evergreen Place's storage options. Moving from a house to apartment. - Eden\n\nLet's discuss storage solutions. What items do you need to store? - Jake\n\nMainly seasonal items, some sports equipment, and extra furniture. - Eden", true},

		{"Considering The Grove but need to verify appliance quality. Love to cook. - Finley\n\nHappy to detail kitchen features. What appliances are most important to you? - Jake\n\nNeed good stove/oven setup, full-size fridge, and dishwasher. - Finley", true},

		{"Looking at Harbor View. How's the water pressure in the showers? - Gray\n\nWater pressure is consistent throughout. Do you have specific concerns about bathroom fixtures? - Jake\n\nYes, need good pressure for morning routine, current place is weak. - Gray", true},

		{"The Irving interested me but want to know about noise between units. - Harper\n\nLet's discuss sound insulation. What types of noise are you most sensitive to? - Jake\n\nMainly concerned about TV/music from neighbors and footsteps above. - Harper", true},

		{"Question about The Jefferson's maintenance response times. - Indigo\n\nHappy to explain our maintenance process. What types of issues concern you most? - Jake\n\nMostly worried about plumbing or AC issues, need quick response. - Indigo", true},

		{"Kenwood Apartments look nice. How's the elevator reliability? - Jordan\n\nWe maintain our elevators regularly. Which floor are you interested in? - Jake\n\nLooking at 8th floor, need reliable elevator service. - Jordan", true},

		{"The Liberty building looks promising. Questions about bike storage. - Kennedy\n\nHappy to discuss bike facilities. What type of bike security do you need? - Jake\n\nHave an expensive e-bike, need secure indoor storage. - Kennedy", true},

		{"Looking at Maple Grove. How are the kitchen counter spaces? - London\n\nLet's discuss kitchen layout. What's your typical cooking style? - Jake\n\nLike to meal prep, need good counter space and storage. - London", true},

		{"The Newport caught my eye. Questions about guest parking. - Morgan\n\nHappy to explain parking policies. How often do you have overnight guests? - Jake\n\nWeekend visitors mainly, need parking for 1-2 extra cars. - Morgan", true},

		{"Interested in Oak Park but concerned about winter maintenance. - Noel\n\nWe have comprehensive winter services. What specific concerns do you have? - Jake\n\nMainly worried about ice removal and parking lot clearing. - Noel", true},

		{"The Parkside looks nice. Questions about laundry facilities. - Ocean\n\nHappy to detail laundry options. Do you prefer in-unit or community facilities? - Jake\n\nDefinitely want in-unit, washer/dryer a must-have. - Ocean", true},

		{"Looking at The Quay. How's the view from upper floors? - Phoenix\n\nCan describe views from different heights. Any specific exposure preference? - Jake\n\nInterested in city views, preferably facing west. - Phoenix", true},

		{"The Regent interests me. Questions about balcony sizes. - Quinn\n\nHappy to discuss outdoor spaces. How do you plan to use the balcony? - Jake\n\nWant space for small garden and seating area. - Quinn", true},

		{"Considering The Strand. How's the bathroom ventilation? - River\n\nCan detail bathroom features. Any specific ventilation concerns? - Jake\n\nNeed good fan system, current place gets too steamy. - River", true},

		{"The Tower looks promising. Questions about deliveries. - Salem\n\nHappy to explain delivery procedures. What types of packages do you receive? - Jake\n\nLots of Amazon, some perishable deliveries too. - Salem", true},

		{"Interested in The Union. How's the proximity to transit? - Taylor\n\nCan detail transit options. Which lines do you typically use? - Jake\n\nNeed access to blue line, prefer walking distance. - Taylor", true},

		{"The Vista looks nice. Questions about utility connections. - Unity\n\nHappy to explain utility setup. Which services are priorities? - Jake\n\nMainly internet and electric, need smooth transition. - Unity", true},

		{"Looking at The Wharf. How's the water view? - Val\n\nCan describe different view options. What floor level interests you? - Jake\n\nInterested in higher floors, want harbor views. - Val", true},

		{"The Xavier building caught my eye. Questions about security. - Winter\n\nHappy to detail security features. Any specific concerns? - Jake\n\nWant controlled access and camera coverage. - Winter", true},

		{"Interested in The Yale. How's the kitchen lighting? - Yael\n\nCan describe lighting options. What type of lighting do you prefer? - Jake\n\nNeed good task lighting for food prep. - Yael", true},

		{"The Zenith looks promising. Questions about closet space. - Zephyr\n\nHappy to discuss storage. What types of closet space do you need? - Jake\n\nNeed walk-in if possible, lots of clothes storage. - Zephyr", true},

		{"Looking at Alpine Views. How's the heating system? - Aspen\n\nCan detail climate control. What temperature do you typically prefer? - Jake\n\nLike it warm in winter, worried about heating costs. - Aspen", true},

		{"The Beacon interests me. Questions about pet areas. - Brook\n\nHappy to discuss pet facilities. What type of pet do you have? - Jake\n\nHave a medium-sized dog, need good walking areas. - Brook", true},

		{"Considering The Crest. How's the bathroom size? - Cedar\n\nCan describe bathroom layouts. What features are important? - Jake\n\nNeed double vanity if possible, good storage. - Cedar", true},

		{"The Delta looks nice. Questions about noise insulation. - Dawn\n\nHappy to discuss soundproofing. What noise concerns you most? - Jake\n\nWorried about street noise, live music nearby. - Dawn", true},

		{"Interested in The Echo. How's the pool maintenance? - Eden\n\nCan detail pool upkeep. How often do you swim? - Jake\n\nDaily swimmer, need clean, well-maintained pool. - Eden", true},

		{"The Flame catches my eye. Questions about grilling areas. - Flint\n\nHappy to discuss outdoor amenities. What type of grilling do you do? - Jake\n\nLike to grill weekly, need gas grill access. - Flint", true},

		{"Looking at The Grove. How's the guest policy? - Glenn\n\nCan explain guest rules. How often do you have visitors? - Jake\n\nFrequent weekend guests, sometimes extended stays. - Glenn", true},

		{"The Haven seems nice. Questions about package handling. - Hope\n\nHappy to detail package procedures. What deliveries do you expect? - Jake\n\nRegular online shopping, some large packages. - Hope", true},

		{"Interested in The Isle. How's the lobby security? - Iris\n\nCan describe security measures. What features matter most? - Jake\n\nWant 24/7 monitoring, secure entry system. - Iris", true},

		{"The Jade looks good. Questions about recycling. - Jasper\n\nHappy to discuss waste management. What do you typically recycle? - Jake\n\nLots of paper and plastics, need convenient bins. - Jasper", true},

		{"Looking at The Key. How's the fitness center? - Kit\n\nCan detail gym equipment. What's your workout routine? - Jake\n\nMainly strength training, need free weights. - Kit", true},

		{"The Lake house interests me. Questions about parking. - Luna\n\nHappy to explain parking options. How many vehicles do you have? - Jake\n\nTwo cars, need covered parking if possible. - Luna", true},

		{"Considering The Mesa. How's the air conditioning? - Mist\n\nCan describe cooling system. What temperature do you prefer? - Jake\n\nLike it cool in summer, worried about efficiency. - Mist", true},

		{"The North Point looks nice. Questions about storage. - Nova\n\nHappy to discuss storage options. What needs to be stored? - Jake\n\nBikes, seasonal items, extra furniture. - Nova", true},

		{"Interested in The Oasis. How's the water quality? - Ocean\n\nCan detail water system. Any specific concerns? - Jake\n\nNeed good drinking water, prefer filtered. - Ocean", true},

		{"The Peak caught my eye. Questions about windows. - Phoenix\n\nHappy to discuss window features. What's important to you? - Jake\n\nNeed good natural light, easy to clean. - Phoenix", true},
		{"Looking at The Quartz. How's the door security? - Quinn\n\nCan describe entry system. What security features matter most? - Jake\n\nWant deadbolts, maybe smart locks. - Quinn", true},
		{"The Ridge seems nice. Questions about mail delivery. - Rain\n\nHappy to explain mail system. What type of mail do you receive? - Jake\n\nRegular packages, some important documents. - Rain", true},
		{"Hi, I noticed your Riverpointe listing. Is parking included?\n\nParking is $50/month per spot. How many vehicles do you have?\n\nJust one car. Is the spot covered or uncovered?\n\nAll spots are covered with security cameras. -Manager", true},
		{"Hello, are dogs allowed at The Metropolitan?\n\nYes, we're pet-friendly. What kind of dog do you have?\n\nA 45lb Lab mix. Any breed or weight restrictions?\n\nNo breed restrictions, just need vaccination records. -Leasing Team", true},
		{"Question about utilities at Park Place Apts\n\nHappy to explain. Which utilities are you asking about?\n\nMainly electric and water costs?\n\nWater included, electric averages $60-80/month. -Office", true},
		{"Are washers/dryers included at The Monarch?\n\nAll units have hookups. Did you want in-unit laundry?\n\nYes, that's important to me. Any units with them installed?\n\nOur deluxe units come with new W/D. -Management", true},
		{"Do units at The Summit have balconies?\n\nYes, most units have private balconies. Which floor interests you?\n\nLooking at 3rd floor. How big are they?\n\n3rd floor balconies are 6x8 feet. -Leasing", true},
		{"What's the pet deposit at Hillside?\n\n$300 pet deposit plus $25 monthly pet rent. What pet do you have?\n\nTwo cats. Is that per pet?\n\nDeposit is per unit, pet rent per pet. -Office", true},
		{"Interested in Grove Park - guest parking?\n\nWe have designated visitor spots. How often do you have guests?\n\nWeekend visitors mainly. Are spots always available?\n\nRarely full, especially weekends. -Management", true},
		{"Security deposit amount for The Chase?\n\nEqual to one month's rent. When were you looking to move?\n\nSeptember 1st. Is it refundable?\n\nFully refundable with normal wear/tear. -Leasing", true},
		{"Do Parkview units have dishwashers?\n\nYes, all units have dishwashers. Looking for any specific features?\n\nJust making sure - had to hand wash at last place.\n\nAll kitchen appliances included here. -Office", true},
		{"Hi, lease length at The Wellington?\n\n12 months standard. When did you want to start?\n\nOctober 1st. Any shorter terms available?\n\n6-month option but higher rate. -Management", true},
		{"Cedar Ridge - fitness center hours?\n\n24/7 access with key fob. What equipment do you use?\n\nMainly weights and treadmill. Is it busy?\n\nRarely crowded, peak times 5-7pm. -Leasing", true},
		{"Application fee for The Victoria?\n\n$50 per adult. Planning to apply soon?\n\nYes, this week. What's needed to apply?\n\nID, proof of income, rental history. -Office", true},
		{"Noise levels at downtown lofts?\n\nWell-insulated units. Any specific concerns?\n\nStreet noise mainly. Which side is quieter?\n\nCourtyard-facing units are quietest. -Management", true},
		{"Square footage of 2BR at The Bristol?\n\n950 sq ft. Would you like to tour?\n\nYes - available this weekend?\n\nShowing Sat 10-4, Sun 12-3. -Leasing", true},
		{"Storage units at Lakeside Manor?\n\nYes, $50/month. What size needed?\n\nMedium-sized for seasonal items.\n\n5x10 units available now. -Office", true},
		{"Internet providers at The Renaissance?\n\nFiber and cable options. Need specific speeds?\n\nWork from home - need reliable high-speed.\n\nGigabit fiber available. -Management", true},
		{"Move-in costs for The Avalon?\n\nFirst month, security deposit, $200 admin fee. When moving?\n\nNext month. Any move-in specials?\n\nWaiving admin fee this month. -Leasing", true},
		{"Woodlands - package delivery system?\n\nSecure package room with 24/7 access. Expecting deliveries?\n\nYes, work from home so frequent packages.\n\nEmail notifications when packages arrive. -Office", true},
		{"Ceiling height at The Manhattan?\n\n9ft standard, 10ft on top floor. Which floor interested in?\n\nTop floor. All units same layout?\n\nPenthouse layouts slightly different. -Management", true},
		{"Hi, central air at The Cypress?\n\nYes, all units have central AC/heat. Any HVAC concerns?\n\nCurrent place struggles in summer.\n\nNew energy-efficient systems here. -Leasing", true},
		{"Parking options at The Victoria?\n\nValet parking $25/day. Need covered parking?\n\nYes, looking for covered spot.\n\nCovered parking $50/month. -Office", true},
		{"Apartment sizes at The Renaissance?\n\nStudio to 3BR. Interested in specific size?\n\n2BR, looking for large living space.\n\n2BR units have 1200 sq ft. -Management", true},
		{"Security measures at The Avalon?\n\n24/7 security, video surveillance. Concerns?\n\nNo, feel safe with these measures.\n\nAdditional security features available. -Leasing", true},
	}
}
